# Runtime image for swarg e2e runs. The swarg binary is copied in by run.sh.
FROM alpine:3.22
RUN apk add --no-cache bash curl git python3 \
 && adduser -D -u 1000 agent \
 && mkdir /work && chown agent:agent /work \
 && mkdir -p /etc/swarg-sandbox/swarg \
 && printf '{"tools":{"auto_approve":true}}\n' > /etc/swarg-sandbox/swarg/config.json
COPY swarg /usr/local/bin/swarg
COPY selfcheck.sh /usr/local/bin/selfcheck.sh
COPY entry.sh /usr/local/bin/entry.sh
USER agent
WORKDIR /work
