# Demo/runtime image: pre-built static binary (no build stage).
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /data
COPY tokencontrolplane /tokencontrolplane
ENV DB_PATH=/data/gateway.db
ENV ADDR=:8080
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/tokencontrolplane"]
