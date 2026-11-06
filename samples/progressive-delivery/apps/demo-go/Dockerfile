FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod main.go ./
RUN CGO_ENABLED=0 go build -o /demo-go .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /demo-go /demo-go
EXPOSE 8080
ENTRYPOINT ["/demo-go"]
