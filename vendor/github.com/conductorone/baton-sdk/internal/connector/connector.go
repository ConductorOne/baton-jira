package connector

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"sync"
	"time"

	grpc_zap "github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	connectorV2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	connectorwrapperV1 "github.com/conductorone/baton-sdk/pb/c1/connector_wrapper/v1"
	ratelimitV1 "github.com/conductorone/baton-sdk/pb/c1/ratelimit/v1"
	tlsV1 "github.com/conductorone/baton-sdk/pb/c1/utls/v1"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	ratelimit2 "github.com/conductorone/baton-sdk/pkg/ratelimit"
	"github.com/conductorone/baton-sdk/pkg/types"
	"github.com/conductorone/baton-sdk/pkg/ugrpc"
	utls2 "github.com/conductorone/baton-sdk/pkg/utls"
)

const listenerFdEnv = "BATON_CONNECTOR_SERVICE_LISTENER_FD"

func WithSyncID(ctx context.Context, syncID string) context.Context {
	return context.WithValue(ctx, types.SyncIDKey{}, syncID)
}

// activeSyncUnaryInterceptor adds ActiveSync annotations to requests if syncID is present in context (set by syncer).
// This is used by the session storage layer transparently.
func activeSyncUnaryInterceptor(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	// Check if syncID is present in context
	syncID := types.GetSyncID(ctx)

	if syncID != "" && req != nil {
		addActiveSyncToRequest(req, syncID)
	}
	return invoker(ctx, method, req, reply, cc, opts...)
}

func addActiveSyncToRequest(req interface{}, syncID string) {
	if req == nil || syncID == "" {
		return
	}

	// Use reflection to check if the request has an Annotations field
	reqValue := reflect.ValueOf(req)
	if reqValue.Kind() == reflect.Ptr {
		reqValue = reqValue.Elem()
	}

	annotationsField := reqValue.FieldByName("Annotations")
	if !annotationsField.IsValid() {
		return
	}

	// Check if the field is of the correct type
	if annotationsField.Type() != reflect.TypeOf(annotations.Annotations{}) &&
		annotationsField.Type() != reflect.TypeOf([]*anypb.Any{}) {
		return
	}

	// Get the current annotations
	var currentAnnotations annotations.Annotations
	if !annotationsField.IsNil() {
		// Handle both annotations.Annotations and []*anypb.Any types
		if annotationsField.Type() == reflect.TypeOf(annotations.Annotations{}) {
			currentAnnotations = annotationsField.Interface().(annotations.Annotations)
		} else {
			// Convert []*anypb.Any to annotations.Annotations
			anySlice := annotationsField.Interface().([]*anypb.Any)
			currentAnnotations = annotations.Annotations(anySlice)
		}
	}

	// Create ActiveSync annotation
	activeSync := &connectorV2.ActiveSync{
		Id: syncID,
	}

	// Add the ActiveSync annotation
	currentAnnotations.Update(activeSync)
	annotationsField.Set(reflect.ValueOf(currentAnnotations))
}

// activeSyncStreamInterceptor adds ActiveSync annotations to streaming requests if syncID is present in context.
func activeSyncStreamInterceptor(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	// For streaming, we need to wrap the stream to intercept the first message
	clientStream, err := streamer(ctx, desc, cc, method, opts...)
	if err != nil {
		return nil, err
	}

	// Check if syncID is present in context
	syncID := types.GetSyncID(ctx)
	if syncID == "" {
		// No syncID, proceed normally
		return clientStream, err
	}

	return &activeSyncClientStream{
		ClientStream: clientStream,
		syncID:       syncID,
	}, nil
}

type activeSyncClientStream struct {
	grpc.ClientStream
	syncID string
}

func (s *activeSyncClientStream) SendMsg(m interface{}) error {
	// Add ActiveSync annotation to the message before sending
	if m != nil {
		addActiveSyncToRequest(m, s.syncID)
	}
	return s.ClientStream.SendMsg(m)
}

// annotationExtractionUnaryInterceptor extracts annotations from requests and adds syncID to context.
// This is used by the server side to make request annotations and syncID available to handlers.
func annotationExtractionUnaryInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	ctx = connectorbuilder.WithAnnotationsFromRequest(ctx, req)
	return handler(ctx, req)
}

// annotationExtractionStreamInterceptor extracts annotations from streaming requests and adds syncID to context.
func annotationExtractionStreamInterceptor(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	// For streaming, we need to wrap the stream to intercept messages
	wrappedStream := &annotationExtractionServerStream{
		ServerStream: ss,
		ctx:          ss.Context(),
	}

	return handler(srv, wrappedStream)
}

type annotationExtractionServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *annotationExtractionServerStream) Context() context.Context {
	return s.ctx
}

func (s *annotationExtractionServerStream) RecvMsg(m interface{}) error {
	err := s.ServerStream.RecvMsg(m)
	if err != nil {
		return err
	}

	// Extract annotations from the message and update context
	annos := connectorbuilder.ExtractAnnotationsFromRequest(m)
	if len(annos) > 0 {
		s.ctx = connectorbuilder.WithAnnotationsFromRequest(s.ctx, m)

		// Extract syncID from ActiveSync annotation if present
		syncID, err := annotations.GetActiveSyncIdFromAnnotations(annos)
		if err == nil && syncID != "" {
			s.ctx = WithSyncID(s.ctx, syncID)
		}
	}

	return nil
}

type connectorClient struct {
	connectorV2.ResourceTypesServiceClient
	connectorV2.ResourcesServiceClient
	connectorV2.ResourceGetterServiceClient
	connectorV2.EntitlementsServiceClient
	connectorV2.GrantsServiceClient
	connectorV2.ConnectorServiceClient
	connectorV2.AssetServiceClient
	ratelimitV1.RateLimiterServiceClient
	connectorV2.GrantManagerServiceClient
	connectorV2.ResourceManagerServiceClient
	connectorV2.ResourceDeleterServiceClient
	connectorV2.AccountManagerServiceClient
	connectorV2.CredentialManagerServiceClient
	connectorV2.EventServiceClient
	connectorV2.TicketsServiceClient
	connectorV2.ActionServiceClient
}

var ErrConnectorNotImplemented = errors.New("client does not implement connector connectorV2")

type wrapper struct {
	mtx sync.RWMutex

	server                  types.ConnectorServer
	client                  types.ConnectorClient
	serverStdin             io.WriteCloser
	conn                    *grpc.ClientConn
	provisioningEnabled     bool
	ticketingEnabled        bool
	fullSyncDisabled        bool
	targetedSyncResourceIDs []string

	rateLimiter   ratelimitV1.RateLimiterServiceServer
	rlCfg         *ratelimitV1.RateLimiterConfig
	rlDescriptors []*ratelimitV1.RateLimitDescriptors_Entry

	now func() time.Time
}

type Option func(ctx context.Context, w *wrapper) error

func WithRateLimiterConfig(cfg *ratelimitV1.RateLimiterConfig) Option {
	return func(ctx context.Context, w *wrapper) error {
		if cfg != nil {
			w.rlCfg = cfg
		}

		return nil
	}
}

func WithRateLimitDescriptor(entry *ratelimitV1.RateLimitDescriptors_Entry) Option {
	return func(ctx context.Context, w *wrapper) error {
		if entry != nil {
			w.rlDescriptors = append(w.rlDescriptors, entry)
		}

		return nil
	}
}

func WithProvisioningEnabled() Option {
	return func(ctx context.Context, w *wrapper) error {
		w.provisioningEnabled = true

		return nil
	}
}

func WithFullSyncDisabled() Option {
	return func(ctx context.Context, w *wrapper) error {
		w.fullSyncDisabled = true
		return nil
	}
}

func WithTicketingEnabled() Option {
	return func(ctx context.Context, w *wrapper) error {
		w.ticketingEnabled = true

		return nil
	}
}

func WithTargetedSyncResourceIDs(resourceIDs []string) Option {
	return func(ctx context.Context, w *wrapper) error {
		w.targetedSyncResourceIDs = resourceIDs
		return nil
	}
}

// NewConnectorWrapper returns a connector wrapper for running connector services locally.
func NewWrapper(ctx context.Context, server interface{}, opts ...Option) (*wrapper, error) {
	connectorServer, isServer := server.(types.ConnectorServer)
	if !isServer {
		return nil, ErrConnectorNotImplemented
	}

	w := &wrapper{
		server: connectorServer,
		now:    time.Now,
	}

	for _, o := range opts {
		err := o(ctx, w)
		if err != nil {
			return nil, err
		}
	}

	return w, nil
}

func (cw *wrapper) Run(ctx context.Context, serverCfg *connectorwrapperV1.ServerConfig) error {
	logger := ctxzap.Extract(ctx)

	l, err := cw.getListener(ctx, serverCfg)
	if err != nil {
		return err
	}

	tlsConfig, err := utls2.ListenerConfig(ctx, serverCfg.Credential)
	if err != nil {
		return err
	}

	grpc_zap.ReplaceGrpcLoggerV2(logger)

	// Get default interceptors and add our custom annotation extraction interceptor
	defaultUnaryInterceptors := ugrpc.UnaryServerInterceptor(ctx)
	customUnaryInterceptors := append(defaultUnaryInterceptors, annotationExtractionUnaryInterceptor)

	defaultStreamInterceptors := ugrpc.StreamServerInterceptors(ctx)
	customStreamInterceptors := append(defaultStreamInterceptors, annotationExtractionStreamInterceptor)

	server := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(tlsConfig)),
		grpc.ChainUnaryInterceptor(customUnaryInterceptors...),
		grpc.ChainStreamInterceptor(customStreamInterceptors...),
		grpc.StatsHandler(otelgrpc.NewServerHandler(
			otelgrpc.WithPropagators(
				propagation.NewCompositeTextMapPropagator(
					propagation.TraceContext{},
					propagation.Baggage{},
				),
			),
		)),
	)

	rl, err := ratelimit2.NewLimiter(ctx, cw.now, serverCfg.RateLimiterConfig)
	if err != nil {
		return err
	}
	cw.rateLimiter = rl
	opts := &RegisterOps{
		Ratelimiter:         cw.rateLimiter,
		ProvisioningEnabled: cw.provisioningEnabled,
		TicketingEnabled:    cw.ticketingEnabled,
	}
	Register(ctx, server, cw.server, opts)
	return server.Serve(l)
}

func (cw *wrapper) runServer(ctx context.Context, serverCred *tlsV1.Credential) (uint32, error) {
	l := ctxzap.Extract(ctx)

	if cw.serverStdin != nil {
		return 0, fmt.Errorf("server is already running")
	}

	listenPort, listener, err := cw.setupListener(ctx)
	if err != nil {
		return 0, err
	}

	serverCfg, err := proto.Marshal(&connectorwrapperV1.ServerConfig{
		Credential:        serverCred,
		RateLimiterConfig: cw.rlCfg,
		ListenPort:        listenPort,
	})
	if err != nil {
		return 0, err
	}

	// Pass all the arguments and append grpc to start the server
	// Config happens via flags, env, or a file. The environment and file should still be
	// available for the subprocess, so this ensures any manually passed flags are also passed
	args := os.Args[1:]
	args = append(args, "_connector-service")

	arg0, err := os.Executable()
	if err != nil {
		return 0, err
	}

	cmd := exec.CommandContext(ctx, arg0, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Make the server config available via stdin
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return 0, err
	}
	_, err = io.WriteString(stdin, base64.StdEncoding.EncodeToString(serverCfg)+"\n")
	if err != nil {
		return 0, err
	}
	cw.serverStdin = stdin

	if listener != nil {
		cmd.ExtraFiles = []*os.File{listener}
		cmd.Env = append(os.Environ(), fmt.Sprintf("%s=3", listenerFdEnv))
	}

	err = cmd.Start()
	if err != nil {
		return 0, err
	}

	go func() {
		waitErr := cmd.Wait()
		if waitErr != nil {
			l.Error("connector service quit unexpectedly", zap.Error(waitErr))
			waitErr = cw.Close()
			if waitErr != nil {
				l.Error("error closing connector wrapper", zap.Error(waitErr))
			}
			os.Exit(1)
		}
	}()

	return listenPort, nil
}

// C returns a ConnectorClient that the caller can use to interact with a locally running connector.
func (cw *wrapper) C(ctx context.Context) (types.ConnectorClient, error) {
	// Check to see if we have a client already
	cw.mtx.RLock()
	if cw.client != nil {
		cw.mtx.RUnlock()
		return cw.client, nil
	}
	cw.mtx.RUnlock()

	// No client, so lets create one
	cw.mtx.Lock()
	defer cw.mtx.Unlock()

	// We have the write lock now, so double check someone else didn't create a client for us.
	if cw.client != nil {
		return cw.client, nil
	}

	// If we don't have an active client, we need to start a sub process to run the server.
	// The subprocess will receive configuration via stdin in the form of a protobuf
	clientCred, serverCred, err := utls2.GenerateClientServerCredentials(ctx)
	if err != nil {
		return nil, err
	}
	clientTLSConfig, err := utls2.ClientConfig(ctx, clientCred)
	if err != nil {
		return nil, err
	}

	listenPort, err := cw.runServer(ctx, serverCred)
	if err != nil {
		return nil, err
	}

	// The server won't start up immediately, so we may need to retry connecting
	// This allows retrying connecting for 5 seconds every 500ms. Once initially
	// connected, grpc will handle retries for us.
	dialCtx, canc := context.WithTimeout(ctx, 5*time.Second)
	defer canc()
	var dialErr error
	var conn *grpc.ClientConn
	for {
		conn, err = grpc.DialContext( //nolint:staticcheck // grpc.DialContext is deprecated but we are using it still.
			ctx,
			fmt.Sprintf("127.0.0.1:%d", listenPort),
			grpc.WithTransportCredentials(credentials.NewTLS(clientTLSConfig)),
			grpc.WithBlock(), //nolint:staticcheck // grpc.WithBlock is deprecated but we are using it still.
			grpc.WithChainUnaryInterceptor(
				ratelimit2.UnaryInterceptor(cw.now, cw.rlDescriptors...),
				activeSyncUnaryInterceptor,
			),
			grpc.WithChainStreamInterceptor(activeSyncStreamInterceptor),
			grpc.WithStatsHandler(otelgrpc.NewClientHandler(
				otelgrpc.WithPropagators(
					propagation.NewCompositeTextMapPropagator(
						propagation.TraceContext{},
						propagation.Baggage{},
					),
				),
			)),
		)
		if err != nil {
			dialErr = err
			select {
			case <-time.After(time.Millisecond * 500):
			case <-dialCtx.Done():
				return nil, dialErr
			}
			continue
		}
		break
	}

	cw.conn = conn
	cw.client = NewConnectorClient(ctx, cw.conn)
	return cw.client, nil
}

// Close shuts down the grpc server and closes the connection.
func (cw *wrapper) Close() error {
	cw.mtx.Lock()
	defer cw.mtx.Unlock()

	var err error
	if cw.conn != nil {
		err = cw.conn.Close()
		if err != nil {
			return fmt.Errorf("error closing client connection: %w", err)
		}
	}

	if cw.serverStdin != nil {
		err = cw.serverStdin.Close()
		if err != nil && errors.Is(err, os.ErrClosed) {
			return fmt.Errorf("error closing connector service stdin: %w", err)
		}
	}

	cw.client = nil
	cw.server = nil
	cw.serverStdin = nil
	cw.conn = nil

	return nil
}

type RegisterOps struct {
	Ratelimiter         ratelimitV1.RateLimiterServiceServer
	ProvisioningEnabled bool
	TicketingEnabled    bool
}

func Register(ctx context.Context, s grpc.ServiceRegistrar, srv types.ConnectorServer, opts *RegisterOps) {
	if opts == nil {
		opts = &RegisterOps{}
	}

	connectorV2.RegisterConnectorServiceServer(s, srv)
	connectorV2.RegisterGrantsServiceServer(s, srv)
	connectorV2.RegisterEntitlementsServiceServer(s, srv)
	connectorV2.RegisterResourcesServiceServer(s, srv)
	connectorV2.RegisterResourceTypesServiceServer(s, srv)
	connectorV2.RegisterAssetServiceServer(s, srv)
	connectorV2.RegisterEventServiceServer(s, srv)
	connectorV2.RegisterResourceGetterServiceServer(s, srv)

	if opts.TicketingEnabled {
		connectorV2.RegisterTicketsServiceServer(s, srv)
	} else {
		noop := &noopTicketing{}
		connectorV2.RegisterTicketsServiceServer(s, noop)
	}

	connectorV2.RegisterActionServiceServer(s, srv)

	if opts.ProvisioningEnabled {
		connectorV2.RegisterGrantManagerServiceServer(s, srv)
		connectorV2.RegisterResourceManagerServiceServer(s, srv)
		connectorV2.RegisterResourceDeleterServiceServer(s, srv)
		connectorV2.RegisterAccountManagerServiceServer(s, srv)
		connectorV2.RegisterCredentialManagerServiceServer(s, srv)
	} else {
		noop := &noopProvisioner{}
		connectorV2.RegisterGrantManagerServiceServer(s, noop)
		connectorV2.RegisterResourceManagerServiceServer(s, noop)
		connectorV2.RegisterResourceDeleterServiceServer(s, noop)
		connectorV2.RegisterAccountManagerServiceServer(s, noop)
		connectorV2.RegisterCredentialManagerServiceServer(s, noop)
	}

	if opts.Ratelimiter != nil {
		ratelimitV1.RegisterRateLimiterServiceServer(s, opts.Ratelimiter)
	}
}

// NewConnectorClient takes a grpc.ClientConnInterface and returns an implementation of the ConnectorClient interface.
// It does not check that the connection actually supports the services.
func NewConnectorClient(ctx context.Context, cc grpc.ClientConnInterface) types.ConnectorClient {
	return &connectorClient{
		ResourceTypesServiceClient:     connectorV2.NewResourceTypesServiceClient(cc),
		ResourcesServiceClient:         connectorV2.NewResourcesServiceClient(cc),
		EntitlementsServiceClient:      connectorV2.NewEntitlementsServiceClient(cc),
		GrantsServiceClient:            connectorV2.NewGrantsServiceClient(cc),
		ConnectorServiceClient:         connectorV2.NewConnectorServiceClient(cc),
		AssetServiceClient:             connectorV2.NewAssetServiceClient(cc),
		RateLimiterServiceClient:       ratelimitV1.NewRateLimiterServiceClient(cc),
		GrantManagerServiceClient:      connectorV2.NewGrantManagerServiceClient(cc),
		ResourceManagerServiceClient:   connectorV2.NewResourceManagerServiceClient(cc),
		ResourceDeleterServiceClient:   connectorV2.NewResourceDeleterServiceClient(cc),
		AccountManagerServiceClient:    connectorV2.NewAccountManagerServiceClient(cc),
		CredentialManagerServiceClient: connectorV2.NewCredentialManagerServiceClient(cc),
		EventServiceClient:             connectorV2.NewEventServiceClient(cc),
		TicketsServiceClient:           connectorV2.NewTicketsServiceClient(cc),
		ActionServiceClient:            connectorV2.NewActionServiceClient(cc),
		ResourceGetterServiceClient:    connectorV2.NewResourceGetterServiceClient(cc),
	}
}
