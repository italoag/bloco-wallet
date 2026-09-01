FROM public.ecr.aws/docker/library/golang:1.26.7-alpine@sha256:28d89ee9cc0ff9fec75c82ca201e6bf7fdf9a679d4b7b24dfa04f2bb766bb468 AS builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG GIT_REV=unknown
ARG BUILD_DATE=unknown
ARG GOPROXY=https://proxy.golang.org,direct
ARG GOSUMDB=sum.golang.org

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath \
    -tags=netgo,osusergo,nocgo,timetzdata \
    -ldflags="-w -s -X main.version=${VERSION} -X main.commit=${GIT_REV} -X main.date=${BUILD_DATE}" \
    -o /out/bloco-wallet-manager \
    ./cmd/blocowallet
RUN /out/bloco-wallet-manager release-smoke \
    && mkdir -p /runtime/home/bloco /runtime/data/tmp \
    && chown -R 65534:65534 /runtime

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder --chown=65534:65534 /runtime/home/bloco /home/bloco
COPY --from=builder --chown=65534:65534 /runtime/data /data
COPY --from=builder /out/bloco-wallet-manager /usr/local/bin/bloco-wallet-manager

ENV HOME=/home/bloco \
    TMPDIR=/data/tmp \
    TZ=UTC \
    BLOCO_WALLET_APP_APP_DIR=/data
VOLUME ["/data"]
USER 65534:65534
ENTRYPOINT ["/usr/local/bin/bloco-wallet-manager"]
