FROM golang:1.19

ADD . /go/src/github.com/netlify/git-gateway

RUN useradd -m netlify && cd /go/src/github.com/netlify/git-gateway && make deps build && mv git-gateway /usr/local/bin/

USER netlify
CMD ["git-gateway"]
