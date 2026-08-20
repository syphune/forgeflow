FROM golang:1.26.6-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGET=api
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/forgeflow ./cmd/${TARGET}
RUN mkdir -p /src/attachment-data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/forgeflow /forgeflow
COPY --from=build /src/db/migrations /migrations
COPY --from=build --chown=65532:65532 /src/attachment-data /var/lib/forgeflow/attachments
ENTRYPOINT ["/forgeflow"]
