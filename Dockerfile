FROM alpine as alpine
RUN apk add -U --no-cache ca-certificates

FROM golang as golang
WORKDIR /work
COPY . .
RUN CGO_ENABLED=0 GOARCH=amd64 GOOS=linux go build -ldflags '-s -w -extldflags "-static"' -o app .

FROM jrottenberg/ffmpeg:4.3-alpine38
COPY --from=golang /work/app /
COPY --from=alpine /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY cred.json /
COPY accounts /accounts
ENTRYPOINT ["/app"]