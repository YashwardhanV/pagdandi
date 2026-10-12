# GraphHopper 11.1 server. Graphs are built by `datactl graph` and mounted at /data.
FROM eclipse-temurin:21-jre-alpine
ARG GH_VERSION=11.1
RUN apk add --no-cache wget \
 && wget -q -O /opt/graphhopper.jar https://github.com/graphhopper/graphhopper/releases/download/${GH_VERSION}/graphhopper-web-${GH_VERSION}.jar
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
EXPOSE 8989
ENTRYPOINT ["/entrypoint.sh"]
