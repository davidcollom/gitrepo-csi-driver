FROM alpine:3.23

RUN apk add --no-cache ca-certificates git git-daemon

COPY git-http-server /usr/local/bin/git-http-server
COPY repo.git /srv/repo.git

ENV GIT_HTTP_EXPORT_ALL=1
ENV GIT_PROJECT_ROOT=/srv

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/git-http-server"]
