export EV_DATA = mem:
# export EV_HTTP = :8080

run:
	go run .
test:
	go test -cover -race ./...


GQLDIR=api/gql_ev
GQLS=$(wildcard $(GQLDIR)/*.go) $(wildcard $(GQLDIR)/*.graphqls) gqlgen.yml
GQLSRC=internal/ev/graph/generated/generated.go

gen: gql
gql: $(GQLSRC)
$(GQLSRC): $(GQLS)
	go get github.com/99designs/gqlgen@latest
	go run github.com/99designs/gqlgen