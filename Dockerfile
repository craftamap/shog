FROM golang:latest

WORKDIR /app
COPY . /app/

RUN go get
RUN go build

FROM debian:buster
WORKDIR /app
COPY --from=0 /app/shog /app/
CMD ["/app/shog"]
