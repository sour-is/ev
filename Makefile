export PATH:=$(shell go env GOPATH)/bin:$(PATH)
export EV_DATA=mem:
export EV_HTTP=:8080
export EV_TRACE_SAMPLE=always
export EV_TRACE_ENDPOINT=localhost:4318
-include local.mk

air: gen
ifeq (, $(shell which air))
	go install github.com/cosmtrek/air@latest
endif
	air

run:
	go run  .

test:
	go test -cover -race ./...


GQLS=gqlgen.yml
GQLS:=$(GQLS) $(wildcard api/gql_ev/*.go)
GQLS:=$(GQLS) $(wildcard pkg/*/*.graphqls)
GQLS:=$(GQLS) $(wildcard app/*/*.graphqls)
GQLS:=$(GQLS) $(wildcard app/*/*.go)
GQLSRC=internal/graph/generated/generated.go

gen: gql
gql: $(GQLSRC)
$(GQLSRC): $(GQLS)
ifeq (, $(shell which gqlgen))
	go install github.com/99designs/gqlgen@latest
endif
	gqlgen


EV_HOST?=localhost:8080
load:
	watch -n .1 "http POST $(EV_HOST)/inbox/asdf/test a=b one=1 two:='{\"v\":2}' | jq"
