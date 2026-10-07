# Compilación
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/planner-service .

# Imagen final: sólo el binario, sin el código ni el compilador
FROM alpine:3.20
RUN adduser -D -H app
USER app
COPY --from=build /out/planner-service /usr/local/bin/planner-service
ENV PORT=8082
EXPOSE 8082
ENTRYPOINT ["/usr/local/bin/planner-service"]
