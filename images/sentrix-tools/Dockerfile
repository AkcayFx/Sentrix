FROM kalilinux/kali-rolling AS base

ENV DEBIAN_FRONTEND=noninteractive

# ── System packages & APT-based security tools ──────────────────────────────
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl git wget unzip \
    python3 python3-pip python3-venv pipx \
    nmap masscan nikto hydra \
    john hashcat \
    tshark tcpdump \
    metasploit-framework exploitdb \
    wfuzz recon-ng theharvester \
    golang-go \
    dnsutils iputils-ping traceroute netcat-openbsd iproute2 libcap2-bin \
    && rm -rf /var/lib/apt/lists/*

# Optional: strip file capabilities so the image also works if someone
# re-enables no-new-privileges in the future.
RUN setcap -r /usr/lib/nmap/nmap 2>/dev/null || true

# ── Go-based tools ──────────────────────────────────────────────────────────
ENV GOPATH=/opt/go
ENV PIPX_HOME=/opt/pipx
ENV PIPX_BIN_DIR=/usr/local/bin
ENV PATH="${PIPX_BIN_DIR}:${GOPATH}/bin:${PATH}"

RUN go install github.com/projectdiscovery/subfinder/v2/cmd/subfinder@latest && \
    go install github.com/owasp-amass/amass/v4/...@latest && \
    go install github.com/ffuf/ffuf/v2@latest && \
    go install github.com/OJ/gobuster/v3@latest && \
    rm -rf /opt/go/pkg /root/.cache/go-build

# ── Python-based tools ─────────────────────────────────────────────────────
RUN pipx install sqlmap && \
    pipx install wapiti3

# XSStrike (not on PyPI — clone from GitHub)
RUN git clone --depth 1 https://github.com/s0md3v/XSStrike.git /opt/xsstrike && \
    cd /opt/xsstrike && python3 -m venv .venv && \
    .venv/bin/pip install -r requirements.txt && \
    ln -s /opt/xsstrike/.venv/bin/python3 /usr/local/bin/xsstrike-python
COPY xsstrike-wrapper.sh /usr/local/bin/xsstrike
RUN chmod +x /usr/local/bin/xsstrike

# ── Non-root user ──────────────────────────────────────────────────────────
RUN id -u operator >/dev/null 2>&1 || useradd -m -s /bin/bash -u 1000 -g operator operator
RUN mkdir -p /work && chown operator:operator /work && \
    ln -s /work /workspace

USER operator
WORKDIR /work

CMD ["sleep", "infinity"]
