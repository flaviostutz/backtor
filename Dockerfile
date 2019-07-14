FROM golang:1.12.3 AS BUILD

RUN apt-get update && apt-get install -y libgeos-dev

RUN mkdir /backtor
WORKDIR /backtor

ADD go.mod .
ADD go.sum .
RUN go mod download

#now build source code
ADD . ./
RUN go build -o /go/bin/backtor



FROM golang:1.12.3

VOLUME [ "/var/lib/backtor/data" ]

ENV CONDUCTOR_API_URL       ''
ENV DATA_DIR                '/var/lib/backtor/data'

COPY --from=BUILD /go/bin/* /bin/
ADD startup.sh /

CMD [ "/startup.sh" ]


# FROM BUILD AS TEST
# RUN go test -v backtor
