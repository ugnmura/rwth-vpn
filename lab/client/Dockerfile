FROM debian:stable-slim
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      openconnect vpnc-scripts ocproxy python3 openssh-client rsync oathtool iproute2 procps \
      ca-certificates iputils-ping netcat-openbsd && rm -rf /var/lib/apt/lists/*
COPY ca.crt /usr/local/share/ca-certificates/rwtest-ca.crt
RUN update-ca-certificates && useradd -m -s /bin/bash tester
CMD ["sleep", "infinity"]
