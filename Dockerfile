FROM golang:1.11.10-alpine3.9 AS build
RUN apk --no-cache add gcc g++ make ca-certificates

WORKDIR /go/src

COPY vendor/ ./

WORKDIR /go/src/gRPC_Redis_Pub

COPY memolock memolock
COPY main.go main.go
COPY service.go service.go
COPY service.pb.go service.pb.go
COPY utils.go utils.go


RUN go install ./...

FROM alpine:3.9
RUN apk --no-cache add curl
EXPOSE 8082/tcp
WORKDIR /usr/bin
COPY --from=build /go/bin .
