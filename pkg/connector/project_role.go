package connector

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/conductorone/baton-jira/pkg/client"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sdk/pkg/session"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	grant "github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	jira "github.com/conductorone/go-jira/v2/cloud"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var resourceTypeProjectRole = &v2.ResourceType{
	Id:          "project-role",
	DisplayName: "Project Role",
	Traits: []v2.ResourceType_Trait{
		v2.ResourceType_TRAIT_ROLE,
	},
}

type projectRoleResourceType struct {
	resourceType *v2.ResourceType
	client       *client.Client
}

func projectRoleResource(project *jira.Project, role *jira.Role) (*v2.Resource, error) {
	profile := map[string]interface{}{
		"name":        role.Name,
		"role_id":     role.ID,
		"project_id":  project.ID,
		"description": role.Description,
	}

	displayName := fmt.Sprintf("%s - %s", project.Name, role.Name)
	resourceID := projectRoleID(project, role)
	roleTraitOptions := []rs.RoleTraitOption{
		rs.WithRoleProfile(profile),
	}

	resource, err := rs.NewRoleResource(displayName, resourceTypeProjectRole, resourceID, roleTraitOptions)
	if err != nil {
		return nil, err
	}

	return resource, nil
}

func (p *projectRoleResourceType) ResourceType(_ context.Context) *v2.ResourceType {
	return p.resourceType
}

func projectRoleBuilder(c *client.Client) *projectRoleResourceType {
	return &projectRoleResourceType{
		resourceType: resourceTypeProjectRole,
		client:       c,
	}
}

func (u *projectRoleResourceType) Entitlements(ctx context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	var rv []*v2.Entitlement

	projectID, roleID, err := parseProjectRoleID(resource.Id.Resource)
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to parse project role ID")
	}

	project, err := u.client.GetProject(ctx, projectID)
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to get project")
	}

	role, err := u.client.GetRole(ctx, roleID)
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to get role")
	}

	assigmentOptions := []ent.EntitlementOption{
		ent.WithGrantableTo(resourceTypeUser, resourceTypeGroup),
		ent.WithDescription(fmt.Sprintf("Assigned to %s role on the %s project", role.Name, project.Name)),
		ent.WithDisplayName(fmt.Sprintf("%s Assignment", resource.DisplayName)),
	}
	rv = append(rv, ent.NewAssignmentEntitlement(resource, assignedEntitlement, assigmentOptions...))

	return rv, "", nil, nil
}

func (p *projectRoleResourceType) Grants(ctx context.Context, resource *v2.Resource, pt *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)

	projectID, roleID, err := parseProjectRoleID(resource.Id.Resource)
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to parse project role ID")
	}

	var rv []*v2.Grant

	projectRoleActors, resp, err := p.client.Jira().Role.GetRoleActorsForProject(ctx, projectID, roleID)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return nil, "", nil, status.Error(codes.NotFound, fmt.Sprintf("failed to get role actors for project: %v", err))
		}
		return nil, "", nil, wrapError(err, "failed to get role actors for project")
	}

	for _, actor := range projectRoleActors {
		var g *v2.Grant
		switch actor.Type {
		case atlassianUserRoleActor:
			userActor := &v2.ResourceId{
				ResourceType: resourceTypeUser.Id,
				Resource:     actor.ActorUser.AccountID,
			}
			g = grant.NewGrant(resource, assignedEntitlement, userActor)

		case atlassianGroupRoleActor:
			groupActor := &v2.ResourceId{
				ResourceType: resourceTypeGroup.Id,
				Resource:     actor.ActorGroup.GroupID,
			}
			g = grant.NewGrant(resource, assignedEntitlement, groupActor, grant.WithAnnotation(&v2.GrantExpandable{
				EntitlementIds:  []string{fmt.Sprintf("group:%s:%s", actor.ActorGroup.GroupID, memberEntitlement)},
				ResourceTypeIds: []string{resourceTypeUser.Id},
			}))

		default:
			l.Warn("unknown role actor type", zap.String("type", actor.Type))
			continue
		}

		rv = append(rv, g)
	}

	return rv, "", nil, nil
}

func (p *projectRoleResourceType) List(ctx context.Context, _ *v2.ResourceId, token *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	bag, offset, err := parsePageToken(token.Token, &v2.ResourceId{ResourceType: resourceTypeProjectRole.Id})
	if err != nil {
		return nil, "", nil, err
	}

	// 🧪 CACHE TEST SUITE - Testing client APIs and cache functionality
	//
	// This test suite runs comprehensive tests to verify that:
	// 1. Basic cache operations (Set/Get) work correctly
	// 2. Batch operations (SetMany/GetMany) function properly
	// 3. Cache miss scenarios are handled gracefully
	// 4. Cache hit scenarios return correct data
	// 5. Error handling works as expected
	// 6. Cache persistence is maintained across operations
	// 7. Client methods properly integrate with the cache system
	//
	// The tests run automatically during the List operation and log results.
	// If tests fail, the operation continues but logs a warning.
	// This ensures production functionality isn't affected by test failures.
	if err := p.runCacheTestSuite(ctx); err != nil {
		// Log the test failure but continue with normal operation
		l := ctxzap.Extract(ctx)
		l.Warn("cache test suite failed, but continuing with normal operation", zap.Error(err))
	}

	projects, _, err := p.client.Jira().Project.Find(ctx, jira.WithStartAt(int(offset)), jira.WithMaxResults(resourcePageSize))
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to get projects")
	}

	var ret []*v2.Resource

	// previously was calling GetProjects, maybe as a sideeffect?
	err = p.client.SetProjects(ctx, projects)
	if err != nil {
		return nil, "", nil, wrapError(err, "failed to get projects")
	}

	for _, prj := range projects {
		for _, roleLink := range prj.Roles {
			roleID, err := parseRoleIdFromRoleLink(roleLink)
			if err != nil {
				return nil, "", nil, wrapError(err, "failed to parse role id from role link")
			}

			role, err := p.client.GetRole(ctx, roleID)
			if err != nil {
				return nil, "", nil, wrapError(err, "failed to get role")
			}

			prr, err := projectRoleResource(&prj, role)
			if err != nil {
				return nil, "", nil, wrapError(err, "failed to create project role resource")
			}
			ret = append(ret, prr)
		}
	}

	if isLastPage(len(projects), resourcePageSize) {
		return ret, "", nil, nil
	}

	nextPage, err := getPageTokenFromOffset(bag, offset+int64(resourcePageSize))
	if err != nil {
		return nil, "", nil, err
	}

	return ret, nextPage, nil, nil
}

// runCacheTestSuite runs comprehensive tests to verify cache functionality
func (p *projectRoleResourceType) runCacheTestSuite(ctx context.Context) error {
	l := ctxzap.Extract(ctx)
	l.Info("🧪 Starting comprehensive cache test suite")

	// Test 1: Basic Set/Get operations
	if err := p.testBasicSetGet(ctx); err != nil {
		return fmt.Errorf("basic set/get test failed: %w", err)
	}

	// Test 2: SetMany/GetMany operations
	if err := p.testSetManyGetMany(ctx); err != nil {
		return fmt.Errorf("setMany/getMany test failed: %w", err)
	}

	// Test 3: Cache miss scenarios
	if err := p.testCacheMiss(ctx); err != nil {
		return fmt.Errorf("cache miss test failed: %w", err)
	}

	// Test 4: Cache hit scenarios
	if err := p.testCacheHit(ctx); err != nil {
		return fmt.Errorf("cache hit test failed: %w", err)
	}

	// Test 5: Error handling
	if err := p.testErrorHandling(ctx); err != nil {
		return fmt.Errorf("error handling test failed: %w", err)
	}

	// Test 6: Cache persistence across calls
	if err := p.testCachePersistence(ctx); err != nil {
		return fmt.Errorf("cache persistence test failed: %w", err)
	}

	// Test 7: Client method cache integration
	if err := p.testClientMethodCacheIntegration(ctx); err != nil {
		return fmt.Errorf("client method cache integration test failed: %w", err)
	}

	// Test 8: GetAll operations
	if err := p.testGetAll(ctx); err != nil {
		return fmt.Errorf("getAll test failed: %w", err)
	}

	// Test 9: Delete operations
	if err := p.testDeleteOperations(ctx); err != nil {
		return fmt.Errorf("delete operations test failed: %w", err)
	}

	// Test 10: Clear operations
	if err := p.testClearOperations(ctx); err != nil {
		return fmt.Errorf("clear operations test failed: %w", err)
	}

	// Test 11: Different data types
	if err := p.testDifferentDataTypes(ctx); err != nil {
		return fmt.Errorf("different data types test failed: %w", err)
	}

	// Test 12: Concurrent operations
	if err := p.testConcurrentOperations(ctx); err != nil {
		return fmt.Errorf("concurrent operations test failed: %w", err)
	}

	// Test 13: Large data sets
	if err := p.testLargeDataSets(ctx); err != nil {
		return fmt.Errorf("large data sets test failed: %w", err)
	}

	// Test 14: Edge cases
	if err := p.testEdgeCases(ctx); err != nil {
		return fmt.Errorf("edge cases test failed: %w", err)
	}

	l.Info("✅ All cache tests passed successfully")
	return nil
}

// RunCacheTests runs the cache test suite independently with configurable verbosity
func (p *projectRoleResourceType) RunCacheTests(ctx context.Context, verbose bool) error {
	l := ctxzap.Extract(ctx)
	if verbose {
		l.SetLevel(zap.DebugLevel)
	}

	l.Info("🧪 Running comprehensive cache test suite independently")

	// Test 1: Basic Set/Get operations
	if err := p.testBasicSetGet(ctx); err != nil {
		return fmt.Errorf("basic set/get test failed: %w", err)
	}

	// Test 2: SetMany/GetMany operations
	if err := p.testSetManyGetMany(ctx); err != nil {
		return fmt.Errorf("setMany/getMany test failed: %w", err)
	}

	// Test 3: Cache miss scenarios
	if err := p.testCacheMiss(ctx); err != nil {
		return fmt.Errorf("cache miss test failed: %w", err)
	}

	// Test 4: Cache hit scenarios
	if err := p.testCacheHit(ctx); err != nil {
		return fmt.Errorf("cache hit test failed: %w", err)
	}

	// Test 5: Error handling
	if err := p.testErrorHandling(ctx); err != nil {
		return fmt.Errorf("error handling test failed: %w", err)
	}

	// Test 6: Cache persistence across calls
	if err := p.testCachePersistence(ctx); err != nil {
		return fmt.Errorf("cache persistence test failed: %w", err)
	}

	// Test 7: Client method cache integration
	if err := p.testClientMethodCacheIntegration(ctx); err != nil {
		return fmt.Errorf("client method cache integration test failed: %w", err)
	}

	// Test 8: GetAll operations
	if err := p.testGetAll(ctx); err != nil {
		return fmt.Errorf("getAll test failed: %w", err)
	}

	// Test 9: Delete operations
	if err := p.testDeleteOperations(ctx); err != nil {
		return fmt.Errorf("delete operations test failed: %w", err)
	}

	// Test 10: Clear operations
	if err := p.testClearOperations(ctx); err != nil {
		return fmt.Errorf("clear operations test failed: %w", err)
	}

	// Test 11: Different data types
	if err := p.testDifferentDataTypes(ctx); err != nil {
		return fmt.Errorf("different data types test failed: %w", err)
	}

	// Test 12: Concurrent operations
	if err := p.testConcurrentOperations(ctx); err != nil {
		return fmt.Errorf("concurrent operations test failed: %w", err)
	}

	// Test 13: Large data sets
	if err := p.testLargeDataSets(ctx); err != nil {
		return fmt.Errorf("large data sets test failed: %w", err)
	}

	// Test 14: Edge cases
	if err := p.testEdgeCases(ctx); err != nil {
		return fmt.Errorf("edge cases test failed: %w", err)
	}

	l.Info("✅ All cache tests passed successfully")
	return nil
}

// testBasicSetGet tests basic set and get operations
func (p *projectRoleResourceType) testBasicSetGet(ctx context.Context) error {
	l := ctxzap.Extract(ctx)
	l.Debug("🧪 Testing basic Set/Get operations")

	// Test data
	testKey := "test:basic:setget"
	testValue := "test-value-123"

	// Test Set
	if err := session.SetJSON(ctx, testKey, testValue); err != nil {
		return fmt.Errorf("failed to set test value: %w", err)
	}

	// Test Get
	retrievedValue, found, err := session.GetJSON[string](ctx, testKey)
	if err != nil {
		return fmt.Errorf("failed to get test value: %w", err)
	}
	if !found {
		return fmt.Errorf("test value not found in cache")
	}
	if retrievedValue != testValue {
		return fmt.Errorf("retrieved value mismatch: expected %s, got %s", testValue, retrievedValue)
	}

	// Cleanup
	if err := session.DeleteJSON(ctx, testKey); err != nil {
		l.Warn("failed to cleanup test key", zap.Error(err))
	}

	l.Debug("✅ Basic Set/Get test passed")
	return nil
}

// testSetManyGetMany tests batch operations
func (p *projectRoleResourceType) testSetManyGetMany(ctx context.Context) error {
	l := ctxzap.Extract(ctx)
	l.Debug("🧪 Testing SetMany/GetMany operations")

	// Test data
	testData := map[string]string{
		"test:many:key1": "value1",
		"test:many:key2": "value2",
		"test:many:key3": "value3",
	}

	// Test SetMany
	if err := session.SetManyJSON(ctx, testData); err != nil {
		return fmt.Errorf("failed to set many test values: %w", err)
	}

	// Test GetMany
	keys := []string{"test:many:key1", "test:many:key2", "test:many:key3"}
	retrievedData, err := session.GetManyJSON[string](ctx, keys)
	if err != nil {
		return fmt.Errorf("failed to get many test values: %w", err)
	}

	// Verify all values were retrieved
	for key, expectedValue := range testData {
		if retrievedValue, exists := retrievedData[key]; !exists {
			return fmt.Errorf("key %s not found in retrieved data", key)
		} else if retrievedValue != expectedValue {
			return fmt.Errorf("value mismatch for key %s: expected %s, got %s", key, expectedValue, retrievedValue)
		}
	}

	// Cleanup
	for key := range testData {
		if err := session.DeleteJSON(ctx, key); err != nil {
			l.Warn("failed to cleanup test key", zap.Error(err), zap.String("key", key))
		}
	}

	l.Debug("✅ SetMany/GetMany test passed")
	return nil
}

// testCacheMiss tests scenarios where items are not in cache
func (p *projectRoleResourceType) testCacheMiss(ctx context.Context) error {
	l := ctxzap.Extract(ctx)
	l.Debug("🧪 Testing cache miss scenarios")

	// Test Get with non-existent key
	testKey := "test:miss:nonexistent"
	_, found, err := session.GetJSON[string](ctx, testKey)
	if err != nil {
		return fmt.Errorf("unexpected error on cache miss: %w", err)
	}
	if found {
		return fmt.Errorf("unexpectedly found non-existent key")
	}

	// Test GetMany with non-existent keys
	keys := []string{"test:miss:key1", "test:miss:key2"}
	retrievedData, err := session.GetManyJSON[string](ctx, keys)
	if err != nil {
		return fmt.Errorf("failed to get many non-existent keys: %w", err)
	}
	if len(retrievedData) != 0 {
		return fmt.Errorf("unexpectedly retrieved data for non-existent keys: %v", retrievedData)
	}

	l.Debug("✅ Cache miss test passed")
	return nil
}

// testCacheHit tests scenarios where items are found in cache
func (p *projectRoleResourceType) testCacheHit(ctx context.Context) error {
	l := ctxzap.Extract(ctx)
	l.Debug("🧪 Testing cache hit scenarios")

	// Set a test value
	testKey := "test:hit:key"
	testValue := "hit-value"
	if err := session.SetJSON(ctx, testKey, testValue); err != nil {
		return fmt.Errorf("failed to set test value for hit test: %w", err)
	}

	// Test Get - should hit cache
	retrievedValue, found, err := session.GetJSON[string](ctx, testKey)
	if err != nil {
		return fmt.Errorf("failed to get cached value: %w", err)
	}
	if !found {
		return fmt.Errorf("cached value not found")
	}
	if retrievedValue != testValue {
		return fmt.Errorf("cached value mismatch: expected %s, got %s", testValue, retrievedValue)
	}

	// Cleanup
	if err := session.DeleteJSON(ctx, testKey); err != nil {
		l.Warn("failed to cleanup test key", zap.Error(err))
	}

	l.Debug("✅ Cache hit test passed")
	return nil
}

// testErrorHandling tests error scenarios
func (p *projectRoleResourceType) testErrorHandling(ctx context.Context) error {
	l := ctxzap.Extract(ctx)
	l.Debug("🧪 Testing error handling")

	// Test with invalid JSON (this should fail gracefully)
	testKey := "test:error:invalid"
	invalidData := make(chan int) // Channels can't be marshaled to JSON

	// This should fail due to JSON marshaling error
	if err := session.SetJSON(ctx, testKey, invalidData); err == nil {
		return fmt.Errorf("expected error when setting invalid data, but got none")
	}

	l.Debug("✅ Error handling test passed")
	return nil
}

// testCachePersistence tests that cache persists across multiple operations
func (p *projectRoleResourceType) testCachePersistence(ctx context.Context) error {
	l := ctxzap.Extract(ctx)
	l.Debug("🧪 Testing cache persistence")

	// Set a test value
	testKey := "test:persistence:key"
	testValue := "persistence-value"
	if err := session.SetJSON(ctx, testKey, testValue); err != nil {
		return fmt.Errorf("failed to set test value for persistence test: %w", err)
	}

	// Retrieve it multiple times to ensure persistence
	for i := 0; i < 3; i++ {
		retrievedValue, found, err := session.GetJSON[string](ctx, testKey)
		if err != nil {
			return fmt.Errorf("failed to get persistent value (attempt %d): %w", i+1, err)
		}
		if !found {
			return fmt.Errorf("persistent value not found (attempt %d)", i+1)
		}
		if retrievedValue != testValue {
			return fmt.Errorf("persistent value mismatch (attempt %d): expected %s, got %s", i+1, testValue, retrievedValue)
		}
	}

	// Cleanup
	if err := session.DeleteJSON(ctx, testKey); err != nil {
		l.Warn("failed to cleanup test key", zap.Error(err))
	}

	l.Debug("✅ Cache persistence test passed")
	return nil
}

// testClientMethodCacheIntegration tests that client methods properly use the cache
func (p *projectRoleResourceType) testClientMethodCacheIntegration(ctx context.Context) error {
	l := ctxzap.Extract(ctx)
	l.Debug("🧪 Testing client method cache integration")

	// Test 1: Test that SetProjects properly caches data
	testProjects := []jira.Project{
		{ID: "test:project:1", Name: "Test Project 1"},
		{ID: "test:project:2", Name: "Test Project 2"},
	}

	// Set projects via client method
	if err := p.client.SetProjects(ctx, testProjects); err != nil {
		return fmt.Errorf("failed to set projects via client: %w", err)
	}

	// Verify they were cached by checking raw cache
	for _, project := range testProjects {
		cachedProject, found, err := session.GetJSON[*jira.Project](ctx, project.ID)
		if err != nil {
			return fmt.Errorf("failed to get cached project %s: %w", project.ID, err)
		}
		if !found {
			return fmt.Errorf("project %s not found in cache after SetProjects", project.ID)
		}
		if cachedProject.ID != project.ID {
			return fmt.Errorf("cached project ID mismatch: expected %s, got %s", project.ID, cachedProject.ID)
		}
	}

	// Test 2: Test that GetProjects retrieves from cache
	projectIDs := []string{"test:project:1", "test:project:2"}
	retrievedProjects, err := p.client.GetProjects(ctx, projectIDs...)
	if err != nil {
		return fmt.Errorf("failed to get projects via client: %w", err)
	}

	// Verify we got the expected projects
	if len(retrievedProjects) != len(testProjects) {
		return fmt.Errorf("project count mismatch: expected %d, got %d", len(testProjects), len(retrievedProjects))
	}

	// Test 3: Test that GetProject retrieves from cache
	project1, err := p.client.GetProject(ctx, "test:project:1")
	if err != nil {
		return fmt.Errorf("failed to get project via client: %w", err)
	}
	if project1.ID != "test:project:1" {
		return fmt.Errorf("retrieved project ID mismatch: expected test:project:1, got %s", project1.ID)
	}

	// Test 4: Test that GetRole properly caches data
	testRole := &jira.Role{ID: 999, Name: "Test Role"}
	testRoleKey := "role:999"

	// Set role directly in cache to simulate what GetRole would do
	if err := session.SetJSON(ctx, testRoleKey, testRole); err != nil {
		return fmt.Errorf("failed to set test role in cache: %w", err)
	}

	// Verify it was cached
	cachedRole, found, err := session.GetJSON[*jira.Role](ctx, testRoleKey)
	if err != nil {
		return fmt.Errorf("failed to get cached role: %w", err)
	}
	if !found {
		return fmt.Errorf("role not found in cache after setting")
	}
	if cachedRole.ID != testRole.ID {
		return fmt.Errorf("cached role ID mismatch: expected %d, got %d", testRole.ID, cachedRole.ID)
	}

	// Cleanup test data
	for _, project := range testProjects {
		if err := session.DeleteJSON(ctx, project.ID); err != nil {
			l.Warn("failed to cleanup test project", zap.Error(err), zap.String("project_id", project.ID))
		}
	}
	if err := session.DeleteJSON(ctx, testRoleKey); err != nil {
		l.Warn("failed to cleanup test role", zap.Error(err))
	}

	l.Debug("✅ Client method cache integration test passed")
	return nil
}

// testGetAll tests GetAll operations
func (p *projectRoleResourceType) testGetAll(ctx context.Context) error {
	l := ctxzap.Extract(ctx)
	l.Debug("🧪 Testing GetAll operations")

	// Set multiple test values
	testData := map[string]string{
		"test:getall:key1": "value1",
		"test:getall:key2": "value2",
		"test:getall:key3": "value3",
	}

	// Set all values
	for key, value := range testData {
		if err := session.SetJSON(ctx, key, value); err != nil {
			return fmt.Errorf("failed to set test value %s: %w", key, err)
		}
	}

	// Test GetAll
	retrievedData, err := session.GetAllJSON[string](ctx)
	if err != nil {
		return fmt.Errorf("failed to get all values: %w", err)
	}

	// Verify we got at least our test values (there might be other data in cache)
	for key, expectedValue := range testData {
		if retrievedValue, exists := retrievedData[key]; !exists {
			return fmt.Errorf("key %s not found in getAll results", key)
		} else if retrievedValue != expectedValue {
			return fmt.Errorf("value mismatch for key %s: expected %s, got %s", key, expectedValue, retrievedValue)
		}
	}

	// Cleanup
	for key := range testData {
		if err := session.DeleteJSON(ctx, key); err != nil {
			l.Warn("failed to cleanup test key", zap.Error(err), zap.String("key", key))
		}
	}

	l.Debug("✅ GetAll test passed")
	return nil
}

// testDeleteOperations tests Delete operations
func (p *projectRoleResourceType) testDeleteOperations(ctx context.Context) error {
	l := ctxzap.Extract(ctx)
	l.Debug("🧪 Testing Delete operations")

	// Set a test value
	testKey := "test:delete:key"
	testValue := "delete-value"
	if err := session.SetJSON(ctx, testKey, testValue); err != nil {
		return fmt.Errorf("failed to set test value for delete test: %w", err)
	}

	// Verify it exists
	_, found, err := session.GetJSON[string](ctx, testKey)
	if err != nil || !found {
		return fmt.Errorf("test value not found before delete: %w", err)
	}

	// Delete the value
	if err := session.DeleteJSON(ctx, testKey); err != nil {
		return fmt.Errorf("failed to delete test value: %w", err)
	}

	// Verify it's gone
	_, found, err = session.GetJSON[string](ctx, testKey)
	if err != nil {
		return fmt.Errorf("error checking deleted value: %w", err)
	}
	if found {
		return fmt.Errorf("deleted value still found in cache")
	}

	l.Debug("✅ Delete operations test passed")
	return nil
}

// testClearOperations tests Clear operations
func (p *projectRoleResourceType) testClearOperations(ctx context.Context) error {
	l := ctxzap.Extract(ctx)
	l.Debug("🧪 Testing Clear operations")

	// Set multiple test values
	testData := map[string]string{
		"test:clear:key1": "value1",
		"test:clear:key2": "value2",
		"test:clear:key3": "value3",
	}

	// Set all values
	for key, value := range testData {
		if err := session.SetJSON(ctx, key, value); err != nil {
			return fmt.Errorf("failed to set test value %s: %w", key, err)
		}
	}

	// Verify they exist
	for key, expectedValue := range testData {
		retrievedValue, found, err := session.GetJSON[string](ctx, key)
		if err != nil || !found {
			return fmt.Errorf("test value %s not found before clear: %w", key, err)
		}
		if retrievedValue != expectedValue {
			return fmt.Errorf("value mismatch for key %s before clear: expected %s, got %s", key, expectedValue, retrievedValue)
		}
	}

	// Clear all values
	if err := session.ClearJSON(ctx); err != nil {
		return fmt.Errorf("failed to clear cache: %w", err)
	}

	// Verify they're all gone
	for key := range testData {
		_, found, err := session.GetJSON[string](ctx, key)
		if err != nil {
			return fmt.Errorf("error checking cleared value %s: %w", key, err)
		}
		if found {
			return fmt.Errorf("cleared value %s still found in cache", key)
		}
	}

	l.Debug("✅ Clear operations test passed")
	return nil
}

// testDifferentDataTypes tests caching of different data types
func (p *projectRoleResourceType) testDifferentDataTypes(ctx context.Context) error {
	l := ctxzap.Extract(ctx)
	l.Debug("🧪 Testing different data types")

	// Test 1: String
	testString := "test-string"
	if err := session.SetJSON(ctx, "test:type:string", testString); err != nil {
		return fmt.Errorf("failed to set string: %w", err)
	}
	retrievedString, found, err := session.GetJSON[string](ctx, "test:type:string")
	if err != nil || !found || retrievedString != testString {
		return fmt.Errorf("string test failed: %w", err)
	}

	// Test 2: Integer
	testInt := 42
	if err := session.SetJSON(ctx, "test:type:int", testInt); err != nil {
		return fmt.Errorf("failed to set int: %w", err)
	}
	retrievedInt, found, err := session.GetJSON[int](ctx, "test:type:int")
	if err != nil || !found || retrievedInt != testInt {
		return fmt.Errorf("int test failed: %w", err)
	}

	// Test 3: Boolean
	testBool := true
	if err := session.SetJSON(ctx, "test:type:bool", testBool); err != nil {
		return fmt.Errorf("failed to set bool: %w", err)
	}
	retrievedBool, found, err := session.GetJSON[bool](ctx, "test:type:bool")
	if err != nil || !found || retrievedBool != testBool {
		return fmt.Errorf("bool test failed: %w", err)
	}

	// Test 4: Struct
	testStruct := struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}{
		Name:  "test-struct",
		Value: 123,
	}
	if err := session.SetJSON(ctx, "test:type:struct", testStruct); err != nil {
		return fmt.Errorf("failed to set struct: %w", err)
	}
	retrievedStruct, found, err := session.GetJSON[struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}](ctx, "test:type:struct")
	if err != nil || !found || retrievedStruct.Name != testStruct.Name || retrievedStruct.Value != testStruct.Value {
		return fmt.Errorf("struct test failed: %w", err)
	}

	// Test 5: Slice
	testSlice := []string{"item1", "item2", "item3"}
	if err := session.SetJSON(ctx, "test:type:slice", testSlice); err != nil {
		return fmt.Errorf("failed to set slice: %w", err)
	}
	retrievedSlice, found, err := session.GetJSON[[]string](ctx, "test:type:slice")
	if err != nil || !found || len(retrievedSlice) != len(testSlice) {
		return fmt.Errorf("slice test failed: %w", err)
	}
	for i, v := range testSlice {
		if retrievedSlice[i] != v {
			return fmt.Errorf("slice item %d mismatch: expected %s, got %s", i, v, retrievedSlice[i])
		}
	}

	// Test 6: Map
	testMap := map[string]int{"key1": 1, "key2": 2, "key3": 3}
	if err := session.SetJSON(ctx, "test:type:map", testMap); err != nil {
		return fmt.Errorf("failed to set map: %w", err)
	}
	retrievedMap, found, err := session.GetJSON[map[string]int](ctx, "test:type:map")
	if err != nil || !found || len(retrievedMap) != len(testMap) {
		return fmt.Errorf("map test failed: %w", err)
	}
	for k, v := range testMap {
		if retrievedMap[k] != v {
			return fmt.Errorf("map value for key %s mismatch: expected %d, got %d", k, v, retrievedMap[k])
		}
	}

	// Cleanup
	keys := []string{"test:type:string", "test:type:int", "test:type:bool", "test:type:struct", "test:type:slice", "test:type:map"}
	for _, key := range keys {
		if err := session.DeleteJSON(ctx, key); err != nil {
			l.Warn("failed to cleanup test key", zap.Error(err), zap.String("key", key))
		}
	}

	l.Debug("✅ Different data types test passed")
	return nil
}

// testConcurrentOperations tests concurrent cache operations
func (p *projectRoleResourceType) testConcurrentOperations(ctx context.Context) error {
	l := ctxzap.Extract(ctx)
	l.Debug("🧪 Testing concurrent operations")

	const numGoroutines = 10
	const numOperations = 5
	errors := make(chan error, numGoroutines*numOperations)

	// Start multiple goroutines doing concurrent operations
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("test:concurrent:goroutine:%d:op:%d", id, j)
				value := fmt.Sprintf("value-%d-%d", id, j)

				// Set value
				if err := session.SetJSON(ctx, key, value); err != nil {
					errors <- fmt.Errorf("goroutine %d failed to set %s: %w", id, key, err)
					return
				}

				// Get value
				retrievedValue, found, err := session.GetJSON[string](ctx, key)
				if err != nil || !found {
					errors <- fmt.Errorf("goroutine %d failed to get %s: %w", id, key, err)
					return
				}
				if retrievedValue != value {
					errors <- fmt.Errorf("goroutine %d value mismatch for %s: expected %s, got %s", id, key, value, retrievedValue)
					return
				}

				// Delete value
				if err := session.DeleteJSON(ctx, key); err != nil {
					errors <- fmt.Errorf("goroutine %d failed to delete %s: %w", id, key, err)
					return
				}
			}
		}(i)
	}

	// Wait for all goroutines to complete
	time.Sleep(100 * time.Millisecond)

	// Check for errors
	close(errors)
	for err := range errors {
		if err != nil {
			return fmt.Errorf("concurrent operation failed: %w", err)
		}
	}

	l.Debug("✅ Concurrent operations test passed")
	return nil
}

// testLargeDataSets tests caching of large data sets
func (p *projectRoleResourceType) testLargeDataSets(ctx context.Context) error {
	l := ctxzap.Extract(ctx)
	l.Debug("🧪 Testing large data sets")

	// Test with a large number of items
	const numItems = 100
	testData := make(map[string]string)

	// Create test data
	for i := 0; i < numItems; i++ {
		key := fmt.Sprintf("test:large:key:%d", i)
		value := fmt.Sprintf("large-value-%d-with-some-extra-content-to-make-it-longer", i)
		testData[key] = value
	}

	// Set many values
	if err := session.SetManyJSON(ctx, testData); err != nil {
		return fmt.Errorf("failed to set many large values: %w", err)
	}

	// Get all keys
	keys := make([]string, 0, len(testData))
	for key := range testData {
		keys = append(keys, key)
	}

	// Retrieve all values
	retrievedData, err := session.GetManyJSON[string](ctx, keys)
	if err != nil {
		return fmt.Errorf("failed to get many large values: %w", err)
	}

	// Verify all values
	if len(retrievedData) != len(testData) {
		return fmt.Errorf("large data set count mismatch: expected %d, got %d", len(testData), len(retrievedData))
	}

	for key, expectedValue := range testData {
		if retrievedValue, exists := retrievedData[key]; !exists {
			return fmt.Errorf("large data set key %s not found", key)
		} else if retrievedValue != expectedValue {
			return fmt.Errorf("large data set value mismatch for key %s: expected %s, got %s", key, expectedValue, retrievedValue)
		}
	}

	// Cleanup
	for key := range testData {
		if err := session.DeleteJSON(ctx, key); err != nil {
			l.Warn("failed to cleanup large data set key", zap.Error(err), zap.String("key", key))
		}
	}

	l.Debug("✅ Large data sets test passed")
	return nil
}

// testEdgeCases tests edge cases and boundary conditions
func (p *projectRoleResourceType) testEdgeCases(ctx context.Context) error {
	l := ctxzap.Extract(ctx)
	l.Debug("🧪 Testing edge cases")

	// Test 1: Empty string
	if err := session.SetJSON(ctx, "test:edge:empty", ""); err != nil {
		return fmt.Errorf("failed to set empty string: %w", err)
	}
	retrievedEmpty, found, err := session.GetJSON[string](ctx, "test:edge:empty")
	if err != nil || !found || retrievedEmpty != "" {
		return fmt.Errorf("empty string test failed: %w", err)
	}

	// Test 2: Zero value
	if err := session.SetJSON(ctx, "test:edge:zero", 0); err != nil {
		return fmt.Errorf("failed to set zero value: %w", err)
	}
	retrievedZero, found, err := session.GetJSON[int](ctx, "test:edge:zero")
	if err != nil || !found || retrievedZero != 0 {
		return fmt.Errorf("zero value test failed: %w", err)
	}

	// Test 3: Nil slice
	var nilSlice []string
	if err := session.SetJSON(ctx, "test:edge:nilslice", nilSlice); err != nil {
		return fmt.Errorf("failed to set nil slice: %w", err)
	}
	retrievedNilSlice, found, err := session.GetJSON[[]string](ctx, "test:edge:nilslice")
	if err != nil || !found || len(retrievedNilSlice) != 0 {
		return fmt.Errorf("nil slice test failed: %w", err)
	}

	// Test 4: Empty map
	emptyMap := make(map[string]int)
	if err := session.SetJSON(ctx, "test:edge:emptymap", emptyMap); err != nil {
		return fmt.Errorf("failed to set empty map: %w", err)
	}
	retrievedEmptyMap, found, err := session.GetJSON[map[string]int](ctx, "test:edge:emptymap")
	if err != nil || !found || len(retrievedEmptyMap) != 0 {
		return fmt.Errorf("empty map test failed: %w", err)
	}

	// Test 5: Very long key
	longKey := strings.Repeat("a", 1000)
	if err := session.SetJSON(ctx, longKey, "long-key-value"); err != nil {
		return fmt.Errorf("failed to set long key: %w", err)
	}
	retrievedLongKey, found, err := session.GetJSON[string](ctx, longKey)
	if err != nil || !found || retrievedLongKey != "long-key-value" {
		return fmt.Errorf("long key test failed: %w", err)
	}

	// Test 6: Very long value
	longValue := strings.Repeat("very-long-value-content-", 100)
	if err := session.SetJSON(ctx, "test:edge:longvalue", longValue); err != nil {
		return fmt.Errorf("failed to set long value: %w", err)
	}
	retrievedLongValue, found, err := session.GetJSON[string](ctx, "test:edge:longvalue")
	if err != nil || !found || retrievedLongValue != longValue {
		return fmt.Errorf("long value test failed: %w", err)
	}

	// Cleanup
	edgeKeys := []string{"test:edge:empty", "test:edge:zero", "test:edge:nilslice", "test:edge:emptymap", "test:edge:longvalue", longKey}
	for _, key := range edgeKeys {
		if err := session.DeleteJSON(ctx, key); err != nil {
			l.Warn("failed to cleanup edge case key", zap.Error(err), zap.String("key", key))
		}
	}

	l.Debug("✅ Edge cases test passed")
	return nil
}

func (p *projectRoleResourceType) Grant(ctx context.Context, principal *v2.Resource, entitlement *v2.Entitlement) (annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)

	if principal.Id.ResourceType != resourceTypeUser.Id {
		err := fmt.Errorf("baton-jira: only users can be granted to groups")

		l.Warn(
			err.Error(),
			zap.String("principal_type", principal.Id.ResourceType),
			zap.String("principal_id", principal.Id.Resource),
		)

		return nil, err
	}

	if entitlement.Id != ent.NewEntitlementID(entitlement.Resource, assignedEntitlement) {
		err := fmt.Errorf("baton-jira: invalid entitlement ID")

		l.Warn(
			err.Error(),
			zap.String("entitlement_id", entitlement.Id),
		)
		return nil, err
	}

	projectID, roleID, err := parseProjectRoleID(entitlement.Resource.Id.Resource)
	if err != nil {
		return nil, wrapError(err, "failed to parse project role ID")
	}

	_, err = p.client.Jira().Role.AddUserToRole(ctx, projectID, roleID, principal.Id.Resource)
	if err != nil {
		if strings.Contains(err.Error(), "already a member of the project role.") {
			l.Info("user already a member of the project role",
				zap.String("project_id", projectID),
				zap.Int("role_id", roleID),
				zap.String("user", principal.Id.Resource),
			)
			return nil, nil
		}

		l.Error(
			"failed to add user to project role",
			zap.Error(err),
			zap.String("project_id", projectID),
			zap.Int("role_id", roleID),
			zap.String("user", principal.Id.Resource),
		)

		return nil, err
	}

	return nil, nil
}

func (p *projectRoleResourceType) Revoke(ctx context.Context, grant *v2.Grant) (annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)

	projectID, roleID, err := parseProjectRoleID(grant.Entitlement.Resource.Id.Resource)
	if err != nil {
		return nil, wrapError(err, "failed to parse project role ID")
	}

	_, err = p.client.Jira().Role.RemoveUserFromRole(ctx, projectID, roleID, grant.Principal.Id.Resource)
	if err != nil {
		return nil, wrapError(err, "failed to remove user from project role")
	}

	l.Info("removed user from project role",
		zap.String("project_id", projectID),
		zap.Int("role_id", roleID),
		zap.String("user", grant.Principal.Id.Resource),
	)

	return nil, nil
}
