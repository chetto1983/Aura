"""Minimal S3 admin against Garage: put / delete / list."""
import asyncio, os, sys
from aiobotocore.session import get_session

ENDPOINT = os.environ.get("S3_ENDPOINT", "http://aura-garage:3900")
BUCKET = os.environ.get("S3_BUCKET", "cocoindex-spike")


async def main():
    cmd = sys.argv[1]
    session = get_session()
    async with session.create_client(
        "s3", endpoint_url=ENDPOINT,
        aws_access_key_id=os.environ["S3_ACCESS_KEY"],
        aws_secret_access_key=os.environ["S3_SECRET_KEY"],
        region_name=os.environ.get("S3_REGION", "garage"),
    ) as c:
        if cmd == "put":
            local, key = sys.argv[2], sys.argv[3]
            with open(local, "rb") as f:
                await c.put_object(Bucket=BUCKET, Key=key, Body=f.read())
            print(f"put  {key}")
        elif cmd == "puttext":
            key, text = sys.argv[2], sys.argv[3]
            await c.put_object(Bucket=BUCKET, Key=key, Body=text.encode())
            print(f"put  {key} ({len(text)} bytes)")
        elif cmd == "putmod":
            # Append a PDF comment after %%EOF: parsers ignore it, but the ETag changes,
            # which is exactly the "same file, edited" case we want to detect.
            local, key = sys.argv[2], sys.argv[3]
            with open(local, "rb") as f:
                body = f.read() + b"\n% modificato dallo spike\n"
            await c.put_object(Bucket=BUCKET, Key=key, Body=body)
            print(f"mod  {key} ({len(body)} bytes)")
        elif cmd == "delete":
            await c.delete_object(Bucket=BUCKET, Key=sys.argv[2])
            print(f"del  {sys.argv[2]}")
        elif cmd == "list":
            r = await c.list_objects_v2(Bucket=BUCKET)
            for o in r.get("Contents", []):
                print(f"  {o['Key']:44} {o['Size']:>10,}  etag={o['ETag'].strip(chr(34))[:16]}")
            if not r.get("Contents"):
                print("  (vuoto)")


asyncio.run(main())
