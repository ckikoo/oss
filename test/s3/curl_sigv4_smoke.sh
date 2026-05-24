#!/usr/bin/env bash
set -euo pipefail

command -v curl >/dev/null 2>&1 || {
  echo "curl is required" >&2
  exit 1
}
command -v openssl >/dev/null 2>&1 || {
  echo "openssl is required" >&2
  exit 1
}
command -v python3 >/dev/null 2>&1 || {
  echo "python3 is required" >&2
  exit 1
}

: "${AWS_ACCESS_KEY_ID:?set AWS_ACCESS_KEY_ID}"
: "${AWS_SECRET_ACCESS_KEY:?set AWS_SECRET_ACCESS_KEY}"

S3_ENDPOINT="${S3_ENDPOINT:-http://127.0.0.1:8080}"
AWS_REGION="${AWS_REGION:-us-east-1}"
AWS_SERVICE="${AWS_SERVICE:-s3}"
BUCKET="${S3_BUCKET:-s3-curl-smoke-$(date +%s)-$RANDOM}"
WORKDIR="$(mktemp -d)"

cleanup() {
  set +e
  s3_request DELETE "/$BUCKET/copied.txt" "" "" >/dev/null 2>&1
  s3_request DELETE "/$BUCKET/hello.txt" "" "" >/dev/null 2>&1
  s3_request DELETE "/$BUCKET/multipart.bin" "" "" >/dev/null 2>&1
  s3_request DELETE "/$BUCKET" "" "" >/dev/null 2>&1
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

uri_encode() {
  python3 - "$1" <<'PY'
import sys
from urllib.parse import quote
print(quote(sys.argv[1], safe="-_.~"))
PY
}

canonical_uri() {
  python3 - "$1" <<'PY'
import sys
from urllib.parse import quote
path = sys.argv[1] or "/"
print("/".join(quote(part, safe="-_.~") for part in path.split("/")))
PY
}

canonical_query() {
  local raw="${1:-}"
  python3 - "$raw" <<'PY'
import sys
from urllib.parse import parse_qsl, quote
raw = sys.argv[1]
pairs = parse_qsl(raw, keep_blank_values=True)
pairs.sort(key=lambda item: (item[0], item[1]))
print("&".join(
    quote(k, safe="-_.~") + "=" + quote(v, safe="-_.~")
    for k, v in pairs
))
PY
}

sha256_hex_file() {
  local file="$1"
  if [[ -n "$file" ]]; then
    openssl dgst -sha256 -binary "$file" | xxd -p -c 256
  else
    printf '' | openssl dgst -sha256 -binary | xxd -p -c 256
  fi
}

sha256_hex_string() {
  printf '%s' "$1" | openssl dgst -sha256 -binary | xxd -p -c 256
}

hmac_hex() {
  local hex_key="$1"
  local data="$2"
  printf '%s' "$data" | openssl dgst -sha256 -mac HMAC -macopt "hexkey:$hex_key" -binary | xxd -p -c 256
}

hmac_text_key_hex() {
  local text_key="$1"
  local data="$2"
  printf '%s' "$data" | openssl dgst -sha256 -hmac "$text_key" -binary | xxd -p -c 256
}

signing_key_hex() {
  local date_scope="$1"
  local k_date k_region k_service
  k_date="$(hmac_text_key_hex "AWS4$AWS_SECRET_ACCESS_KEY" "$date_scope")"
  k_region="$(hmac_hex "$k_date" "$AWS_REGION")"
  k_service="$(hmac_hex "$k_region" "$AWS_SERVICE")"
  hmac_hex "$k_service" "aws4_request"
}

s3_request() {
  local method="$1"
  local path="$2"
  local query="${3:-}"
  local body_file="${4:-}"
  shift 4 || true

  local endpoint_no_scheme host url payload_hash amz_date date_scope can_uri can_query
  endpoint_no_scheme="${S3_ENDPOINT#http://}"
  endpoint_no_scheme="${endpoint_no_scheme#https://}"
  host="${endpoint_no_scheme%%/*}"
  if [[ -n "$query" ]]; then
    url="$S3_ENDPOINT$path?$query"
  else
    url="$S3_ENDPOINT$path"
  fi

  payload_hash="$(sha256_hex_file "$body_file")"
  amz_date="$(date -u +%Y%m%dT%H%M%SZ)"
  date_scope="${amz_date:0:8}"
  can_uri="$(canonical_uri "$path")"
  can_query="$(canonical_query "$query")"

  local canonical_request string_to_sign credential_scope signing_key signature auth_header
  canonical_request="$method
$can_uri
$can_query
host:$host
x-amz-content-sha256:$payload_hash
x-amz-date:$amz_date

host;x-amz-content-sha256;x-amz-date
$payload_hash"
  credential_scope="$date_scope/$AWS_REGION/$AWS_SERVICE/aws4_request"
  string_to_sign="AWS4-HMAC-SHA256
$amz_date
$credential_scope
$(sha256_hex_string "$canonical_request")"
  signing_key="$(signing_key_hex "$date_scope")"
  signature="$(hmac_hex "$signing_key" "$string_to_sign")"
  auth_header="AWS4-HMAC-SHA256 Credential=$AWS_ACCESS_KEY_ID/$credential_scope, SignedHeaders=host;x-amz-content-sha256;x-amz-date, Signature=$signature"

  local curl_args=(-fsS)
  if [[ "$method" == "HEAD" ]]; then
    curl_args+=(--head)
  else
    curl_args+=(-X "$method")
  fi

  if [[ -n "$body_file" ]]; then
    curl "${curl_args[@]}" "$url" \
      -H "Authorization: $auth_header" \
      -H "x-amz-date: $amz_date" \
      -H "x-amz-content-sha256: $payload_hash" \
      "$@" \
      --data-binary "@$body_file"
  else
    curl "${curl_args[@]}" "$url" \
      -H "Authorization: $auth_header" \
      -H "x-amz-date: $amz_date" \
      -H "x-amz-content-sha256: $payload_hash" \
      "$@"
  fi
}

echo "[curl s3 smoke] endpoint=$S3_ENDPOINT bucket=$BUCKET"

echo "[curl s3 smoke] list buckets"
s3_request GET "/" "" "" >/dev/null

echo "[curl s3 smoke] create/head bucket"
s3_request PUT "/$BUCKET" "" "" >/dev/null
s3_request HEAD "/$BUCKET" "" "" >/dev/null
s3_request GET "/$BUCKET" "location=" "" >/dev/null

echo "hello curl sigv4 $(date -u +%Y-%m-%dT%H:%M:%SZ)" >"$WORKDIR/hello.txt"

echo "[curl s3 smoke] put/head/get object"
s3_request PUT "/$BUCKET/hello.txt" "" "$WORKDIR/hello.txt" -H "Content-Type: text/plain" >/dev/null
s3_request HEAD "/$BUCKET/hello.txt" "" "" >/dev/null
s3_request GET "/$BUCKET/hello.txt" "" "" -o "$WORKDIR/download.txt" >/dev/null
cmp "$WORKDIR/hello.txt" "$WORKDIR/download.txt"

echo "[curl s3 smoke] list objects v2"
s3_request GET "/$BUCKET" "list-type=2&prefix=hello&max-keys=10" "" >/dev/null

echo "[curl s3 smoke] copy object"
copy_source="/$BUCKET/hello.txt"
s3_request PUT "/$BUCKET/copied.txt" "" "" -H "x-amz-copy-source: $copy_source" >/dev/null
s3_request HEAD "/$BUCKET/copied.txt" "" "" >/dev/null

echo "[curl s3 smoke] multipart upload"
printf 'part-one-%s\n' "$(date -u +%s)" >"$WORKDIR/part1.bin"
printf 'part-two-%s\n' "$(date -u +%s)" >"$WORKDIR/part2.bin"
s3_request POST "/$BUCKET/multipart.bin" "uploads=" "" -H "Content-Type: application/octet-stream" >"$WORKDIR/create_multipart.xml"
UPLOAD_ID="$(python3 - "$WORKDIR/create_multipart.xml" <<'PY'
import sys
import xml.etree.ElementTree as ET
root = ET.parse(sys.argv[1]).getroot()
for elem in root.iter():
    if elem.tag.split('}', 1)[-1] == 'UploadId':
        print(elem.text or '')
        break
PY
)"
if [[ -z "$UPLOAD_ID" ]]; then
  echo "missing multipart upload id" >&2
  exit 1
fi
s3_request PUT "/$BUCKET/multipart.bin" "partNumber=1&uploadId=$UPLOAD_ID" "$WORKDIR/part1.bin" -D "$WORKDIR/part1.headers" >/dev/null
s3_request PUT "/$BUCKET/multipart.bin" "partNumber=2&uploadId=$UPLOAD_ID" "$WORKDIR/part2.bin" -D "$WORKDIR/part2.headers" >/dev/null
ETAG1="$(python3 - "$WORKDIR/part1.headers" <<'PY'
import sys
for line in open(sys.argv[1], encoding='utf-8', errors='ignore'):
    if line.lower().startswith('etag:'):
        print(line.split(':', 1)[1].strip())
        break
PY
)"
ETAG2="$(python3 - "$WORKDIR/part2.headers" <<'PY'
import sys
for line in open(sys.argv[1], encoding='utf-8', errors='ignore'):
    if line.lower().startswith('etag:'):
        print(line.split(':', 1)[1].strip())
        break
PY
)"
if [[ -z "$ETAG1" || -z "$ETAG2" ]]; then
  echo "missing multipart part etag" >&2
  exit 1
fi
s3_request GET "/$BUCKET/multipart.bin" "uploadId=$UPLOAD_ID" "" >/dev/null
cat >"$WORKDIR/complete.xml" <<XML
<CompleteMultipartUpload>
  <Part><PartNumber>1</PartNumber><ETag>$ETAG1</ETag></Part>
  <Part><PartNumber>2</PartNumber><ETag>$ETAG2</ETag></Part>
</CompleteMultipartUpload>
XML
s3_request POST "/$BUCKET/multipart.bin" "uploadId=$UPLOAD_ID" "$WORKDIR/complete.xml" -H "Content-Type: application/xml" >/dev/null
s3_request HEAD "/$BUCKET/multipart.bin" "" "" >/dev/null

echo "[curl s3 smoke] delete objects and bucket"
cat >"$WORKDIR/delete.xml" <<XML
<Delete>
  <Object><Key>copied.txt</Key></Object>
  <Object><Key>hello.txt</Key></Object>
  <Object><Key>multipart.bin</Key></Object>
</Delete>
XML
s3_request POST "/$BUCKET" "delete=" "$WORKDIR/delete.xml" -H "Content-Type: application/xml" >/dev/null
s3_request DELETE "/$BUCKET" "" "" >/dev/null

echo "[curl s3 smoke] ok"
