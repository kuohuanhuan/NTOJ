package ntoj

import com.zaxxer.hikari.HikariConfig
import com.zaxxer.hikari.HikariDataSource
import java.net.URI
import java.net.URLDecoder
import java.sql.Connection
import java.sql.PreparedStatement
import java.sql.ResultSet
import java.sql.SQLException
import java.sql.Types
import java.time.Instant
import java.time.OffsetDateTime
import java.time.ZoneOffset
import javax.sql.DataSource
import org.postgresql.util.PGobject

/** Typed wrappers for query arguments that JDBC cannot infer on its own. */
class IntArr(val values: List<Int>)
class TextArr(val values: List<String>)
class Jsonb(val json: String)

private val PLACEHOLDER = Regex("""\$(\d+)""")

/**
 * A thin layer over one JDBC connection. Queries use PostgreSQL's numbered
 * placeholders ($1, $2, ...), which may repeat; values are always bound as
 * parameters, never spliced into the SQL text.
 */
class Sql(private val conn: Connection) {
    private fun prepare(sql: String, args: Array<out Any?>): PreparedStatement {
        val order = ArrayList<Any?>()
        val text = PLACEHOLDER.replace(sql) { m ->
            order.add(args[m.groupValues[1].toInt() - 1])
            "?"
        }
        val st = conn.prepareStatement(text)
        order.forEachIndexed { idx, v ->
            val i = idx + 1
            when (v) {
                null -> st.setNull(i, Types.NULL)
                is Int -> st.setInt(i, v)
                is Long -> st.setLong(i, v)
                is Double -> st.setDouble(i, v)
                is Boolean -> st.setBoolean(i, v)
                is String -> st.setString(i, v)
                is ByteArray -> st.setBytes(i, v)
                is Instant -> st.setObject(i, OffsetDateTime.ofInstant(v, ZoneOffset.UTC))
                is IntArr -> st.setArray(i, conn.createArrayOf("int4", v.values.toTypedArray()))
                is TextArr -> st.setArray(i, conn.createArrayOf("text", v.values.toTypedArray()))
                is Jsonb -> st.setObject(i, PGobject().apply { type = "jsonb"; value = v.json })
                else -> throw IllegalArgumentException("unsupported SQL argument type ${v::class}")
            }
        }
        return st
    }

    /** Runs a statement and returns the number of affected rows. */
    fun exec(sql: String, vararg args: Any?): Int = prepare(sql, args).use { it.executeUpdate() }

    fun <T> query(sql: String, vararg args: Any?, map: (ResultSet) -> T): List<T> =
        prepare(sql, args).use { st ->
            st.executeQuery().use { rs ->
                val out = ArrayList<T>()
                while (rs.next()) out.add(map(rs))
                out
            }
        }

    /** First row, or null when the query returns none. */
    fun <T : Any> one(sql: String, vararg args: Any?, map: (ResultSet) -> T): T? =
        prepare(sql, args).use { st -> st.executeQuery().use { rs -> if (rs.next()) map(rs) else null } }

    /** The single row of [sql] as a JSON object; 404 when there is none. */
    fun jsonRow(sql: String, vararg args: Any?): String =
        one("SELECT to_jsonb(t)::text FROM ($sql) t", *args) { it.getString(1) } ?: throw notFound()

    /** All rows of [sql] as a JSON array, in query order. */
    fun jsonList(sql: String, vararg args: Any?): String = one(
        "SELECT coalesce(jsonb_agg(to_jsonb(t) - '_n' ORDER BY t._n), '[]'::jsonb)::text " +
            "FROM (SELECT q.*, row_number() OVER () AS _n FROM ($sql) q) t",
        *args,
    ) { it.getString(1) }!!
}

class Db(private val ds: DataSource) : AutoCloseable {
    fun <T> use(block: (Sql) -> T): T = ds.connection.use { block(Sql(it)) }

    /** Runs [block] in a transaction; any exception rolls it back. */
    fun <T> tx(block: (Sql) -> T): T = ds.connection.use { conn ->
        conn.autoCommit = false
        try {
            val result = block(Sql(conn))
            conn.commit()
            result
        } catch (e: Throwable) {
            conn.rollback()
            throw e
        } finally {
            conn.autoCommit = true
        }
    }

    fun exec(sql: String, vararg args: Any?): Int = use { it.exec(sql, *args) }
    fun <T> query(sql: String, vararg args: Any?, map: (ResultSet) -> T): List<T> = use { it.query(sql, *args, map = map) }
    fun <T : Any> one(sql: String, vararg args: Any?, map: (ResultSet) -> T): T? = use { it.one(sql, *args, map = map) }
    fun jsonRow(sql: String, vararg args: Any?): String = use { it.jsonRow(sql, *args) }
    fun jsonList(sql: String, vararg args: Any?): String = use { it.jsonList(sql, *args) }

    /** COUNT(*)-style helper. */
    fun count(sql: String, vararg args: Any?): Int = one(sql, *args) { it.getInt(1) } ?: 0

    override fun close() {
        (ds as? AutoCloseable)?.close()
    }

    /**
     * Applies every migration that has not been recorded yet. Each file runs in
     * its own transaction; an advisory lock keeps concurrent instances from racing.
     */
    fun migrate() = ds.connection.use { conn ->
        val lockId = 0x4e544f4a // "NTOJ"
        conn.createStatement().use { it.execute("SELECT pg_advisory_lock($lockId)") }
        try {
            conn.createStatement().use {
                it.execute("CREATE TABLE IF NOT EXISTS schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())")
            }
            for (name in MIGRATIONS) {
                val sql = Sql(conn)
                if (sql.one("SELECT 1 FROM schema_migrations WHERE name = $1", name) { true } != null) continue
                val text = Db::class.java.getResource("/db/migrations/$name")?.readText()
                    ?: throw IllegalStateException("migration $name is missing from the build")
                conn.autoCommit = false
                try {
                    conn.createStatement().use { it.execute(text) }
                    sql.exec("INSERT INTO schema_migrations (name) VALUES ($1)", name)
                    conn.commit()
                } catch (e: Throwable) {
                    conn.rollback()
                    throw IllegalStateException("migration $name: ${e.message}", e)
                } finally {
                    conn.autoCommit = true
                }
            }
        } finally {
            conn.createStatement().use { it.execute("SELECT pg_advisory_unlock($lockId)") }
        }
    }

    companion object {
        /** Migration files under resources/db/migrations, in the order they apply. */
        val MIGRATIONS = listOf("0001_init.sql")

        /** Opens a pool from a `postgres://user:pass@host:port/db?opts` or `jdbc:postgresql:` URL. */
        fun open(url: String): Db {
            val cfg = HikariConfig()
            if (url.startsWith("jdbc:")) {
                cfg.jdbcUrl = url
            } else {
                val uri = URI(url)
                require(uri.scheme == "postgres" || uri.scheme == "postgresql") { "DATABASE_URL must be a postgres:// URL" }
                val port = if (uri.port > 0) ":${uri.port}" else ""
                cfg.jdbcUrl = "jdbc:postgresql://${uri.host}$port${uri.rawPath ?: "/"}" + (uri.rawQuery?.let { "?$it" } ?: "")
                uri.rawUserInfo?.let { info ->
                    cfg.username = URLDecoder.decode(info.substringBefore(':'), Charsets.UTF_8)
                    if (':' in info) cfg.password = URLDecoder.decode(info.substringAfter(':'), Charsets.UTF_8)
                }
            }
            cfg.maximumPoolSize = 16
            cfg.poolName = "ntoj"
            return Db(HikariDataSource(cfg))
        }
    }
}

fun ResultSet.instant(i: Int): Instant? = getObject(i, OffsetDateTime::class.java)?.toInstant()
fun ResultSet.intOrNull(i: Int): Int? = getInt(i).takeUnless { wasNull() }
fun ResultSet.ints(i: Int): List<Int> = (getArray(i)?.array as? Array<*>)?.map { (it as Number).toInt() } ?: emptyList()
fun ResultSet.strings(i: Int): List<String> = (getArray(i)?.array as? Array<*>)?.map { it as String } ?: emptyList()

fun Throwable.isUniqueViolation() = (this as? SQLException)?.sqlState == "23505"
fun Throwable.isForeignKeyViolation() = (this as? SQLException)?.sqlState == "23503"
