FROM alpine:3.23

RUN apk add --no-cache ca-certificates git

RUN addgroup -S gitcontent && \
    adduser -S -D -H -u 10001 -G gitcontent gitcontent

COPY gitrepo-csi-nodeplugin /usr/local/bin/gitrepo-csi-nodeplugin

USER gitcontent

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/gitrepo-csi-nodeplugin"]
