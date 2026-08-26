FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/sensitive-checker .

FROM alpine:3.22
RUN addgroup -S app && adduser -S -G app app
WORKDIR /app
COPY --from=build /out/sensitive-checker ./sensitive-checker
COPY --from=build /src/templates ./templates
RUN mkdir -p /app/temp /app/data && chown -R app:app /app
USER app
EXPOSE 8008
ENTRYPOINT ["./sensitive-checker"]
