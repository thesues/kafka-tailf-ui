## Summary

golang rewrite of https://github.com/klyr/kafka-tailf-ui


## Build

```
go build

```


## Usage


###

```
./kafka-tailf-ui -broker 10.0.1.1:9090, 101.1.1.1:9090
```
Browse http://localhost:5000

###

```
docker run -d -p5000:5000 --rm thesues/tailf -brokers 10.0.6.131:2223
```
