package dev.photosync.server

/**
 * Minimal JSON writer — avoids pulling in a serialization dependency.
 * Handles nested objects/arrays and guarantees correct comma placement.
 */
class JsonWriter {
    private val sb = StringBuilder(1024)
    /** True when the current container already holds at least one complete value. */
    private var hasValue = false

    fun obj(name: String? = null, block: JsonWriter.() -> Unit): JsonWriter {
        if (name != null) key(name) else forgetComma()
        sb.append('{')
        hasValue = false
        block()
        sb.append('}')
        hasValue = true
        return this
    }

    fun arr(name: String? = null, block: JsonWriter.() -> Unit): JsonWriter {
        if (name != null) key(name) else forgetComma()
        sb.append('[')
        hasValue = false
        block()
        sb.append(']')
        hasValue = true
        return this
    }

    fun field(name: String, value: String?): JsonWriter {
        key(name)
        if (value == null) sb.append("null") else string(value)
        hasValue = true
        return this
    }

    fun field(name: String, value: Number): JsonWriter {
        key(name); sb.append(value); hasValue = true; return this
    }

    fun field(name: String, value: Boolean): JsonWriter {
        key(name); sb.append(value); hasValue = true; return this
    }

    fun nullField(name: String): JsonWriter {
        key(name); sb.append("null"); hasValue = true; return this
    }

    /** Insert pre-rendered JSON as a named value. */
    fun raw(name: String, json: String): JsonWriter {
        key(name); sb.append(json); hasValue = true; return this
    }

    /** Append raw JSON values to the current array. */
    fun rawItem(json: String) {
        forgetComma()
        sb.append(json)
        hasValue = true
    }

    /** Emit the separator if needed, then the quoted key and a colon. */
    private fun key(name: String) {
        if (hasValue) sb.append(',')
        string(name)
        sb.append(':')
        hasValue = false
    }

    /** Called when opening a nameless container: keep the parent's separator state handled. */
    private fun forgetComma() {
        if (hasValue) sb.append(',')
        hasValue = false
    }

    private fun string(s: String) {
        sb.append('"')
        for (c in s) {
            when (c) {
                '"' -> sb.append("\\\"")
                '\\' -> sb.append("\\\\")
                '\n' -> sb.append("\\n")
                '\r' -> sb.append("\\r")
                '\t' -> sb.append("\\t")
                else -> if (c < ' ') sb.append("\\u%04x".format(c.code)) else sb.append(c)
            }
        }
        sb.append('"')
    }

    override fun toString(): String = sb.toString()
}

/** Build a JSON object string, e.g. json { field("a", 1) } -> {"a":1} */
fun json(block: JsonWriter.() -> Unit): String =
    JsonWriter().apply { obj(name = null, block = block) }.toString()

/** Build a JSON array string. */
fun jsonArray(block: JsonWriter.() -> Unit): String =
    JsonWriter().apply { arr(name = null, block = block) }.toString()

/** Reads the few flat fields we need out of a small JSON request body. */
object JsonReader {
    fun string(body: String?, key: String): String? {
        if (body == null) return null
        val m = Regex("\"${Regex.escape(key)}\"\\s*:\\s*\"((?:[^\"\\\\]|\\\\.)*)\"").find(body) ?: return null
        return m.groupValues[1].replace("\\\"", "\"").replace("\\\\", "\\")
    }

    fun long(body: String?, key: String): Long? {
        if (body == null) return null
        val m = Regex("\"${Regex.escape(key)}\"\\s*:\\s*(-?\\d+)").find(body) ?: return null
        return m.groupValues[1].toLongOrNull()
    }
}
