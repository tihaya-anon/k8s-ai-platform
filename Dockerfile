# The binary is built on the host (make build) so the image needs no base layer and no network.
# See docs/mvp.md for why the MVP stays on the standard library.
FROM scratch
COPY bin/platform-linux-amd64 /platform
ENTRYPOINT ["/platform"]
