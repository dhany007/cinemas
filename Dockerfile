FROM golang:1.25-alpine AS api-build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/cinemas-api ./cmd/api

FROM api-build AS seed-build

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/cinemas-seed ./cmd/seed

FROM alpine:3.22 AS api

RUN addgroup -S cinemas && adduser -S cinemas -G cinemas
USER cinemas

COPY --from=api-build /out/cinemas-api /cinemas-api

EXPOSE 8080
ENTRYPOINT ["/cinemas-api"]

FROM alpine:3.22 AS seed

RUN apk add --no-cache ca-certificates && addgroup -S cinemas && adduser -S cinemas -G cinemas
USER cinemas

COPY --from=seed-build /out/cinemas-seed /cinemas-seed

ENTRYPOINT ["/cinemas-seed"]

FROM node:24-alpine AS web-dependencies

WORKDIR /src

COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/customer-web/package.json apps/customer-web/package.json
COPY apps/admin-web/package.json apps/admin-web/package.json
RUN corepack enable && pnpm install --frozen-lockfile

FROM web-dependencies AS customer-web-build

COPY apps/customer-web apps/customer-web
RUN pnpm --filter customer-web build

FROM node:24-alpine AS customer-web

WORKDIR /app
ENV NODE_ENV=production
ENV PORT=3000
ENV HOSTNAME=0.0.0.0
COPY --from=customer-web-build /src/apps/customer-web/.next/standalone ./
COPY --from=customer-web-build /src/apps/customer-web/.next/static ./apps/customer-web/.next/static
EXPOSE 3000
CMD ["node", "apps/customer-web/server.js"]

FROM web-dependencies AS admin-web-build

COPY apps/admin-web apps/admin-web
RUN pnpm --filter admin-web build

FROM node:24-alpine AS admin-web

WORKDIR /app
ENV NODE_ENV=production
ENV PORT=3000
ENV HOSTNAME=0.0.0.0
COPY --from=admin-web-build /src/apps/admin-web/.next/standalone ./
COPY --from=admin-web-build /src/apps/admin-web/.next/static ./apps/admin-web/.next/static
EXPOSE 3000
CMD ["node", "apps/admin-web/server.js"]
