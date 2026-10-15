FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/planerum ./cmd/server

FROM python:3.13-slim-bookworm
WORKDIR /app
COPY office/requirements.txt /app/office/requirements.txt
RUN pip install --no-cache-dir -r /app/office/requirements.txt
COPY --from=build /out/planerum /usr/local/bin/planerum
COPY office/ /app/office/
COPY static/ /app/static/
ENV PORT=8080 ARTIFACT_ROOT=/app/data/artifacts OFFICE_PYTHON=python3
RUN mkdir -p /app/data/artifacts && chown -R 10001:10001 /app/data
USER 10001:10001
EXPOSE 8080
CMD ["planerum"]
