FROM postgres:18-bookworm

RUN apt-get update \
    && apt-get install -y --no-install-recommends python3 python3-pip \
    && pip3 install --no-cache-dir --break-system-packages "patroni[etcd3]==4.0.6" "psycopg[binary]" \
    && apt-get purge -y python3-pip \
    && apt-get autoremove -y \
    && rm -rf /var/lib/apt/lists/*

COPY build/patroni-entrypoint.sh /usr/local/bin/patroni-entrypoint.sh
RUN chmod +x /usr/local/bin/patroni-entrypoint.sh

ENTRYPOINT ["/usr/local/bin/patroni-entrypoint.sh"]
CMD ["/etc/patroni/patroni.yml"]
