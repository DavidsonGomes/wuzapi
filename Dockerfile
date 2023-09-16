FROM golang:1.19-alpine as build

WORKDIR /app

COPY . .

RUN CGO_ENABLED=0 go build -o server .

FROM alpine:3.18.3

WORKDIR /app

COPY --from=build /app .

VOLUME [ "/app/dbdata", "/app/files" ]

ENTRYPOINT [ "/app/server" ]

CMD [ "-logtype", "json" ]
