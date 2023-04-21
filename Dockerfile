FROM golang:1.17.11

RUN mkdir /app
RUN mkdir /data

WORKDIR /app

COPY . .

RUN go build -o ./bin/app.a *.go
CMD ["./bin/app.a", "-dbpath", "/data/dbswarm.db"]

