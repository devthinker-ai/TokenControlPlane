# Multi-stage: Node → Go (CGO=0) → distroless non-root.
FROM node:22-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.25-alpine AS backend
WORKDIR /src
ENV CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/frontend/dist ./pkg/web/dist
ARG VERSION=dev
ARG COMMIT=none
ARG BUILT=unknown
RUN go build -trimpath -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.built=${BUILT}" -o /out/tokencontrolplane ./cmd/gateway

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /data
COPY --from=backend /out/tokencontrolplane /tokencontrolplane
ENV DB_PATH=/data/gateway.db
ENV ADDR=:8080
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/tokencontrolplane"]
