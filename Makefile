export PATH:=$(shell go env GOPATH)/bin:$(PATH)
export EV_DATA=mem:
export EV_HTTP=:8080
#export EV_TRACE_SAMPLE=always
#export EV_TRACE_ENDPOINT=localhost:4318
-include local.mk

air: gen
ifeq (, $(shell which air))
	go install github.com/cosmtrek/air@latest
endif
	air ./cmd/ev

run:
	go build ./cmd/ev && ./ev

test:
	go test -cover -race ./...


GQLS=gqlgen.yml
GQLS:=$(GQLS) $(wildcard api/gql_ev/*.go)
GQLS:=$(GQLS) $(wildcard pkg/*/*.graphqls)
GQLS:=$(GQLS) $(wildcard app/*/*.graphqls)
GQLS:=$(GQLS) $(wildcard app/*/*.go)
GQLSRC=internal/graph/generated/generated.go

clean:
	rm -f "$(GQLSRC)" 
gen: gql
gql: $(GQLSRC)
$(GQLSRC): $(GQLS)
ifeq (, $(shell which gqlgen))
	go install github.com/99designs/gqlgen@latest
endif
	gqlgen

