FROM golang:1.15-alpine as builder

WORKDIR /workspace

COPY . .

RUN go build -o spike-echo && \
    chmod +x spike-echo

FROM alpine:latest

COPY --from=builder /workspace/spike-echo /usr/bin/spike-echo

ENTRYPOINT [ "/usr/bin/spike-echo" ]