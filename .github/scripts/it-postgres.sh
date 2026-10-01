#!/usr/bin/env sh
# Starts PostgreSQL with TLS for the integration tests and prints the DSN.
#
# The DSN uses sslmode=verify-full against a throwaway CA, so the tests run
# the same path as production: Config.Password refuses unverified TLS.
#
#   eval "$(.github/scripts/it-postgres.sh)"  # local; PGPORT picks the host port
#   GITHUB_ENV=... it-postgres.sh            # CI: appends the DSN to GITHUB_ENV
set -eu

name=gofi-it-postgres
port=${PGPORT:-5432}
dir=${RUNNER_TEMP:-${TMPDIR:-/tmp}}/$name
rm -rf "$dir" && mkdir -p "$dir"

# Throwaway CA and a server certificate for localhost.
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj "/CN=gofi-it-ca" \
	-keyout "$dir/ca.key" -out "$dir/ca.crt" 2>/dev/null
openssl req -newkey rsa:2048 -nodes -subj "/CN=localhost" \
	-keyout "$dir/server.key" -out "$dir/server.csr" 2>/dev/null
printf 'subjectAltName=DNS:localhost,IP:127.0.0.1\n' >"$dir/san.ext"
openssl x509 -req -days 1 -in "$dir/server.csr" -CA "$dir/ca.crt" -CAkey "$dir/ca.key" \
	-CAcreateserial -extfile "$dir/san.ext" -out "$dir/server.crt" 2>/dev/null
chmod 644 "$dir/server.key"

docker rm -f "$name" >/dev/null 2>&1 || true
# The key must belong to the postgres user with mode 0600, so it is copied
# inside the container before the official entrypoint starts the server.
docker run -d --name "$name" -p "$port:5432" \
	-e POSTGRES_PASSWORD=pw -e POSTGRES_DB=it \
	-v "$dir:/certs:ro" --entrypoint sh postgres:16-alpine -c '
		install -o postgres -m 600 /certs/server.key /var/lib/postgresql/server.key
		install -o postgres -m 644 /certs/server.crt /var/lib/postgresql/server.crt
		exec docker-entrypoint.sh postgres -c ssl=on \
			-c ssl_cert_file=/var/lib/postgresql/server.crt \
			-c ssl_key_file=/var/lib/postgresql/server.key' >/dev/null

# Over TCP, so the temporary server of the init phase (socket only) does not count.
i=0
until docker exec "$name" pg_isready -q -h 127.0.0.1 -U postgres; do
	i=$((i + 1))
	[ "$i" -lt 60 ] || { docker logs "$name"; exit 1; }
	sleep 1
done

dsn="host=localhost port=$port user=postgres password=pw dbname=it sslmode=verify-full sslrootcert=$dir/ca.crt"
if [ -n "${GITHUB_ENV:-}" ]; then
	echo "GOFI_IT_POSTGRES_DSN=$dsn" >>"$GITHUB_ENV"
else
	echo "export GOFI_IT_POSTGRES_DSN='$dsn'"
fi
