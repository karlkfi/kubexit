FROM golang:1.26-alpine AS build
RUN mkdir /build
WORKDIR /build
COPY . /build/
RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -o test-server ./cmd/test-server

FROM alpine:3.23
RUN apk --no-cache add ca-certificates tzdata curl
COPY --from=build /build/test-server /bin/
EXPOSE 80
ENTRYPOINT ["test-server"]
