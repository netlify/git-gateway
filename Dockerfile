FROM golang:1.11.2-alpine3.8

ADD . /go/src/github.com/netlify/git-gateway

RUN apk add --update alpine-sdk

RUN rm -rf /var/cache/apk/*

RUN go get -u github.com/Masterminds/glide golang.org/x/lint/golint

RUN adduser -D -u 1000 netlify && cd /go/src/github.com/netlify/git-gateway && make deps build lint && mv git-gateway /usr/local/bin/

USER netlify
CMD ["git-gateway"]
