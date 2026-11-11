plugins {
    kotlin("jvm") version "2.4.21"
    kotlin("plugin.serialization") version "2.4.21"
    application
}

group = "ntoj"
version = "1.0.0"

repositories {
    mavenCentral()
}

dependencies {
    implementation("io.ktor:ktor-server-core:3.6.1")
    implementation("io.ktor:ktor-server-netty:3.6.1")
    implementation("org.jetbrains.kotlinx:kotlinx-serialization-json:1.12.0")
    implementation("com.zaxxer:HikariCP:6.3.3")
    implementation("org.postgresql:postgresql:42.7.14")
    implementation("org.bouncycastle:bcprov-jdk18on:1.82")
    runtimeOnly("org.slf4j:slf4j-simple:2.0.20")

    testImplementation(kotlin("test"))
}

kotlin {
    jvmToolchain(21)
}

application {
    mainClass = "ntoj.MainKt"
    applicationName = "ntoj"
}

tasks.test {
    useJUnitPlatform()
    // The API integration tests need a scratch PostgreSQL database.
    environment("TEST_DATABASE_URL", System.getenv("TEST_DATABASE_URL") ?: "")
    testLogging { events("failed"); showStandardStreams = false; exceptionFormat = org.gradle.api.tasks.testing.logging.TestExceptionFormat.FULL }
}
