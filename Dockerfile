# SPDX-License-Identifier: MIT
#
# The review workspace service (cmd/workspace). See docs/workspace.md.
#
# All three base images are pinned by digest. A tag can be moved, and this image
# carries users' GitHub tokens through its memory, so what it is built from has
# to be the thing that was looked at. To update one, change the tag named in the
# comment above it and the digest together.

# node:22-bookworm-slim
#
# Builds the web application. Nothing of this stage reaches the final image
# but the built files: no Node, no npm, no node_modules.
FROM node@sha256:e9f565d32859f0cbfb52125eafe8be39390f24dac302dc3cdf566e8ba3ab21ee AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
# Exactly what the lockfile says, and no package's install script is run.
RUN npm ci --ignore-scripts
COPY web/ ./
# The type-check reads the recorded API responses, which live beside the service.
COPY internal/workspace/api/testdata/examples /src/internal/workspace/api/testdata/examples
RUN npm run build

# golang:1.27
FROM golang@sha256:205c7636d74e0ffbe3d007162df7d74f859f5a8c8322dfb9f907977686c126cf AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg
# The service embeds the web application from this directory.
COPY --from=web /src/web/dist ./internal/workspace/web/dist
ARG VERSION=dev
# Static, so it runs on an image with no C library.
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/workspace ./cmd/workspace

# gcr.io/distroless/static-debian12:nonroot
#
# No shell and no package manager. The service must never run code from a pull
# request, and the strongest form of that is an image with nothing to run code
# with. .github/scripts/check-image.sh holds the image to it in CI.
FROM gcr.io/distroless/static-debian12@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/workspace /usr/local/bin/workspace
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/workspace"]
CMD ["serve", "--config", "/etc/workspace/config.yaml"]
