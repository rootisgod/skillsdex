FROM golang:1.23-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/skillsdexd ./cmd/skillsdexd

FROM debian:bookworm-slim AS runtime

RUN apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates \
	&& rm -rf /var/lib/apt/lists/* \
	&& groupadd --system --gid 10001 skillsdex \
	&& useradd --system --uid 10001 --gid skillsdex --home-dir /data --shell /usr/sbin/nologin skillsdex \
	&& mkdir -p /data \
	&& chown skillsdex:skillsdex /data

COPY --from=build /out/skillsdexd /usr/local/bin/skillsdexd

ENV SKILLSDEX_HOME=/data
EXPOSE 8765
USER skillsdex

ENTRYPOINT ["/usr/local/bin/skillsdexd"]
CMD ["serve", "--addr", "0.0.0.0:8765", "--db", "/data/skillsdex.db"]
