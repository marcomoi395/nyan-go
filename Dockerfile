FROM golang:1.27-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /nyan-go ./cmd/nyan-go

FROM alpine:3.22

RUN apk add --no-cache tzdata
WORKDIR /app
COPY --from=build /nyan-go /usr/local/bin/nyan-go

ENTRYPOINT ["nyan-go"]
