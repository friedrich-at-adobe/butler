FROM alpine:3.5
LABEL maintainer="Stegen Smith <matthsmi@adobe.com>"

RUN apk update && apk add bash

COPY ./butler /butler

ENTRYPOINT ["/butler"]