FROM gcr.io/distroless/static-debian11:nonroot
ENTRYPOINT ["/baton-example"]
COPY baton-example /