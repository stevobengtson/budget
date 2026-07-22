package ca.pigglet.budget

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL

// Models mirror the JSON contracts served by internal/web/apiv1.
data class User(val id: Long, val email: String, val name: String)
data class AuthResult(val token: String, val user: User, val subscribed: Boolean, val addOns: List<String>)
data class MeResult(val user: User, val subscribed: Boolean, val addOns: List<String>)

// Mirrors GET /api/v1/budget. All *Cents fields are integer cents.
data class BudgetSummary(val incomeCents: Long, val budgetedCents: Long, val remainingCents: Long)
data class BudgetCategory(
    val id: Long,
    val name: String,
    val assignedCents: Long,
    val spentCents: Long,
    val availableCents: Long,
    val goalCents: Long?,
    val rolloverMode: String,
)
data class BudgetGroup(val id: Long, val name: String, val categories: List<BudgetCategory>)
data class BudgetData(
    val month: String,
    val prevMonth: String,
    val nextMonth: String,
    val summary: BudgetSummary,
    val groups: List<BudgetGroup>,
)

// ApiException carries the server's error envelope { error: { code, message } }
// or a transport failure, so the UI can show a message and branch on `code`.
class ApiException(val code: String, message: String) : Exception(message)

class ApiClient(private val baseUrl: String) {

    suspend fun login(email: String, password: String): AuthResult = withContext(Dispatchers.IO) {
        val body = JSONObject().put("email", email).put("password", password).toString()
        val json = request("POST", "/api/v1/login", body = body, token = null)
        AuthResult(
            token = json.getString("token"),
            user = json.getJSONObject("user").toUser(),
            subscribed = json.getBoolean("subscribed"),
            addOns = json.stringList("addOns"),
        )
    }

    suspend fun me(token: String): MeResult = withContext(Dispatchers.IO) {
        val json = request("GET", "/api/v1/me", body = null, token = token)
        MeResult(
            user = json.getJSONObject("user").toUser(),
            subscribed = json.getBoolean("subscribed"),
            addOns = json.stringList("addOns"),
        )
    }

    suspend fun logout(token: String): Unit = withContext(Dispatchers.IO) {
        runCatching { request("POST", "/api/v1/logout", body = null, token = token) }
        Unit
    }

    suspend fun budget(token: String, month: String?): BudgetData = withContext(Dispatchers.IO) {
        val path = if (month != null) "/api/v1/budget?month=$month" else "/api/v1/budget"
        request("GET", path, body = null, token = token).toBudget()
    }

    suspend fun assign(token: String, categoryId: Long, month: String, amount: String): BudgetData =
        withContext(Dispatchers.IO) {
            val body = JSONObject()
                .put("categoryId", categoryId)
                .put("month", month)
                .put("amount", amount)
                .toString()
            request("POST", "/api/v1/budget/assign", body = body, token = token).toBudget()
        }

    private fun JSONObject.toUser() = User(getLong("id"), getString("email"), getString("name"))

    private fun JSONObject.toBudget(): BudgetData {
        val s = getJSONObject("summary")
        val summary = BudgetSummary(
            s.getLong("incomeCents"), s.getLong("budgetedCents"), s.getLong("remainingCents"),
        )
        val groupsArr = getJSONArray("groups")
        val groups = List(groupsArr.length()) { i ->
            val g = groupsArr.getJSONObject(i)
            val catsArr = g.getJSONArray("categories")
            val categories = List(catsArr.length()) { j ->
                val c = catsArr.getJSONObject(j)
                BudgetCategory(
                    id = c.getLong("id"),
                    name = c.getString("name"),
                    assignedCents = c.getLong("assignedCents"),
                    spentCents = c.getLong("spentCents"),
                    availableCents = c.getLong("availableCents"),
                    goalCents = if (c.isNull("goalCents")) null else c.getLong("goalCents"),
                    rolloverMode = c.getString("rolloverMode"),
                )
            }
            BudgetGroup(g.getLong("id"), g.getString("name"), categories)
        }
        return BudgetData(
            month = getString("month"),
            prevMonth = getString("prevMonth"),
            nextMonth = getString("nextMonth"),
            summary = summary,
            groups = groups,
        )
    }

    private fun JSONObject.stringList(key: String): List<String> {
        val array = optJSONArray(key) ?: return emptyList()
        return List(array.length()) { array.getString(it) }
    }

    // request performs the call and returns the parsed JSON body, or throws
    // ApiException on a non-2xx response (decoding the error envelope when present).
    private fun request(method: String, path: String, body: String?, token: String?): JSONObject {
        val conn = (URL("$baseUrl$path").openConnection() as HttpURLConnection).apply {
            requestMethod = method
            connectTimeout = 8000
            readTimeout = 8000
            token?.let { setRequestProperty("Authorization", "Bearer $it") }
            if (body != null) {
                setRequestProperty("Content-Type", "application/json")
                doOutput = true
            }
        }
        try {
            if (body != null) conn.outputStream.use { it.write(body.toByteArray()) }
            val code = conn.responseCode
            val text = (if (code in 200..299) conn.inputStream else conn.errorStream)
                ?.bufferedReader()?.use { it.readText() }.orEmpty()
            if (code !in 200..299) throw parseError(code, text)
            return if (text.isBlank()) JSONObject() else JSONObject(text)
        } catch (e: ApiException) {
            throw e
        } catch (e: Exception) {
            throw ApiException("network", "Couldn't reach the server.")
        } finally {
            conn.disconnect()
        }
    }

    private fun parseError(code: Int, text: String): ApiException = try {
        val err = JSONObject(text).getJSONObject("error")
        ApiException(err.getString("code"), err.getString("message"))
    } catch (e: Exception) {
        ApiException("http_$code", "Request failed ($code).")
    }
}
