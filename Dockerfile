FROM golang as builder
WORKDIR /go/src/github.com/SchweizerischeBundesbahnen/ssp-backend
COPY . .
RUN go get -v ./server

FROM centos:7
COPY --from=builder /go/bin/server /usr/local/bin
EXPOSE 8000
ENTRYPOINT server
