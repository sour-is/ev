export EV_DATA = mem:
export EV_HTTP = :8080

run: gen
	go run .
test:
	go test -cover -race ./...


GQLDIR=api/gql_ev
GQLS=$(wildcard $(GQLDIR)/*.go) $(wildcard $(GQLDIR)/*.graphqls) gqlgen.yml
GQLSRC=internal/graph/generated/generated.go

gen: gql
gql: $(GQLSRC)
$(GQLSRC): $(GQLS)
ifeq (, $(shell which gqlgen))
	go install github.com/99designs/gqlgen@latest
endif
	gqlgen

load:
	watch -n .1 "http POST localhost:8080/event/asdf/test a=b one=1 two:='{\"v\":2}' | jq"

bi:
	go build .
	sudo mv ev /usr/local/bin/
	sudo systemctl restart ev
