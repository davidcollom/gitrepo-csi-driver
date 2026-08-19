FROM alpine:3.24

ARG TARGETOS
ARG TARGETARCH

RUN apk add --no-cache ca-certificates git

RUN addgroup -S gitcontent && \
    adduser -S -D -H -u 10001 -G gitcontent gitcontent

WORKDIR /

COPY dist/gitrepo-csi-nodeplugin_${TARGETOS}_${TARGETARCH}/v2/gitrepo-csi-nodeplugin /usr/local/bin/gitrepo-csi-nodeplugin

USER gitcontent

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/gitrepo-csi-nodeplugin"]
