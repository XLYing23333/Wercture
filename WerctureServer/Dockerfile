FROM node:22-bookworm-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27.1-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
ARG WERCTURE_VERSION=0.1.0
RUN CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags="-s -w -X main.version=${WERCTURE_VERSION}" -o /out/wercture-server ./cmd/wercture-server

FROM scratch
COPY --from=build /out/wercture-server /wercture-server
USER 10001:10001
ENTRYPOINT ["/wercture-server"]
CMD ["run", "--data", "/data", "--listen", ":9416"]
