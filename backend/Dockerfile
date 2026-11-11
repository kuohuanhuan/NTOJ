FROM eclipse-temurin:21-jdk AS build
WORKDIR /src
COPY gradlew settings.gradle.kts build.gradle.kts ./
COPY gradle gradle
RUN ./gradlew --no-daemon dependencies > /dev/null
COPY src src
RUN ./gradlew --no-daemon installDist

FROM eclipse-temurin:21-jre-alpine
RUN adduser -D -u 10001 ntoj && mkdir /data && chown ntoj /data
USER ntoj
COPY --from=build /src/build/install/ntoj /opt/ntoj
ENV DATA_DIR=/data LISTEN_ADDR=:8080 JAVA_OPTS="-XX:MaxRAMPercentage=75"
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=20s CMD wget -qO- http://127.0.0.1:8080/api/healthz || exit 1
ENTRYPOINT ["/opt/ntoj/bin/ntoj"]
