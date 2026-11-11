package ntoj

import io.ktor.server.engine.EmbeddedServer
import io.ktor.server.engine.embeddedServer
import io.ktor.server.netty.Netty
import io.ktor.server.netty.NettyApplicationEngine
import java.io.File
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import kotlin.system.exitProcess

/** Builds the HTTP server for [server] without starting it. */
fun httpServer(server: Server, host: String, port: Int): EmbeddedServer<NettyApplicationEngine, NettyApplicationEngine.Configuration> =
    embeddedServer(Netty, port = port, host = host) { server.install(this) }

fun main() {
    val cfg = try {
        Config.load()
    } catch (e: IllegalArgumentException) {
        System.err.println("fatal: ${e.message}")
        exitProcess(1)
    }
    File(cfg.dataDir).mkdirs()
    val db = Db.open(cfg.databaseUrl)
    db.migrate()
    val server = Server(db, cfg)
    bootstrapAdmin(server)
    if (cfg.judgeToken.isEmpty()) {
        server.log.warn("JUDGE_TOKEN is not set: the judge worker API is disabled and submissions stay queued")
    }

    val maintenance = Executors.newSingleThreadScheduledExecutor { r -> Thread(r, "maintenance").apply { isDaemon = true } }
    maintenance.scheduleWithFixedDelay({ runMaintenance(server) }, 0, 1, TimeUnit.MINUTES)

    val http = httpServer(server, cfg.listenHost, cfg.listenPort)
    Runtime.getRuntime().addShutdownHook(
        Thread {
            server.log.info("shutting down")
            http.stop(1_000, 15_000)
            maintenance.shutdownNow()
            db.close()
        },
    )
    server.log.info("listening on {}:{}", cfg.listenHost, cfg.listenPort)
    http.start(wait = true)
}
