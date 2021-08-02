FROM registry.ci.openshift.org/openshift/release:golang-1.15 AS builder
WORKDIR /go/src/github.com/vrutkovs/promecieus
COPY . .
RUN go mod vendor && go build -o ./promecieus ./cmd/promecieus


FROM registry.access.redhat.com/ubi8/ubi-minimal:8.4
COPY --from=builder /go/src/github.com/vrutkovs/promecieus/promecieus /bin/promecieus
COPY --from=builder /go/src/github.com/vrutkovs/promecieus/html /srv/html
WORKDIR /srv
ENTRYPOINT ["/bin/promecieus"]
