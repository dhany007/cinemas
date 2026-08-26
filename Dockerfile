FROM golang:1.25-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/cinemas-api ./cmd/api

FROM alpine:3.22

RUN addgroup -S cinemas && adduser -S cinemas -G cinemas
USER cinemas

COPY --from=build /out/cinemas-api /cinemas-api

EXPOSE 8080
ENTRYPOINT ["/cinemas-api"]
