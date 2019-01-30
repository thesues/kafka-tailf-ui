FROM alpine
WORKDIR /root/
COPY kafka-tailf-ui /root/
COPY assets/ /root/assets
ENTRYPOINT ["./kafka-tailf-ui"]
