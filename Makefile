export EV_DATA = mem:
# export EV_HTTP = :8080

run:
	go run .
test:
	go test -cover -race ./...