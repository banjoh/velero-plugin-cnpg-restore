FROM golang:1.23-bookworm AS build
ENV GOPROXY=https://proxy.golang.org
WORKDIR /go/src/github.com/nvanthao/velero-plugin-cnpg-restore
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /go/bin/velero-plugin-cnpg-restore .

FROM busybox:1.33.1 AS busybox

FROM scratch
COPY --from=build /go/bin/velero-plugin-cnpg-restore /plugins/
COPY --from=busybox /bin/cp /bin/cp
USER 65532:65532
ENTRYPOINT ["cp", "/plugins/velero-plugin-cnpg-restore", "/target/."]
