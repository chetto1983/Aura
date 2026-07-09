FROM python:3.14-slim

RUN pip install --no-cache-dir --root-user-action=ignore turingdb==1.35

EXPOSE 6666
ENTRYPOINT ["turingdb"]
