
FROM golang:1.25 AS build

WORKDIR /go/src/onyxia-janitor

COPY go.mod go.sum ./

COPY . .

RUN go mod download

RUN CGO_ENABLED=0 go build -o /go/bin/onyxia-janitor cmd/main.go

FROM gcr.io/distroless/static-debian12

COPY --from=build /go/bin/onyxia-janitor /

CMD ["/onyxia-janitor"]