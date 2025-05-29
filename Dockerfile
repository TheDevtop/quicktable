FROM golang:alpine
COPY . /go/src/
WORKDIR /go/src
RUN go install ./cmd
RUN rm -rf /home /media /opt /mnt /srv
VOLUME [ "/data" ]
WORKDIR /data
CMD [ "/go/bin/main" ]
ENV QTAB_PATH="/data/" QTAB_ADDR=":8080"
LABEL org.opencontainers.image.source=https://github.com/TheDevtop/quicktable
LABEL org.opencontainers.image.licenses=Apache-2.0
