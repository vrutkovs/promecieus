FROM registry.svc.ci.openshift.org/openshift/release:golang-1.15 AS builder
WORKDIR /go/src/github.com/vrutkovs/promecieus
COPY . .
RUN go mod vendor && go build -o ./promecieus ./cmd/promecieus


FROM registry.access.redhat.com/ubi8/ubi-minimal:8.2
COPY --from=builder /go/src/github.com/vrutkovs/promecieus/promecieus /bin/promecieus
COPY --from=builder /go/src/github.com/vrutkovs/promecieus/html /bin/
RUN mkdir /output && chown 1000:1000 /output
USER 1000:1000
ENV PATH /bin
ENV HOME /output
WORKDIR /output
ENTRYPOINT ["/bin/promecieus"]
