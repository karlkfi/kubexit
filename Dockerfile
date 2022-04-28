FROM golang:1.18-alpine3.15 AS builder
RUN mkdir /build
WORKDIR /build
COPY . /build/
RUN CGO_ENABLED=0 GOOS=linux go build -o kubexit ./cmd/kubexit

FROM alpine:3.15
RUN apk --no-cache add ca-certificates tzdata
COPY --from=builder /build/kubexit /bin/
ENTRYPOINT ["kubexit"]
