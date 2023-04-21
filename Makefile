build:
	@go build -o ./bin/app.a *.go

run: build
	@./bin/app.a

