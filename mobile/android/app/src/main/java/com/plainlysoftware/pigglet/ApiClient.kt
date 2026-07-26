package com.plainlysoftware.pigglet

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

// Mirrors GET /api/v1/accounts and /accounts/:id/transactions.
data class Account(val id: Long, val name: String, val type: String, val balanceCents: Long)

// Mirrors GET /api/v1/accounts?month=… — accounts with balances as of month-end.
data class AccountsPage(
    val month: String,
    val prevMonth: String,
    val nextMonth: String,
    val accounts: List<Account>,
)
data class TransactionItem(
    val id: Long,
    val date: String,
    val payee: String,
    val category: String,
    val categoryId: Long?,
    val notes: String,
    val amountCents: Long,
    val cleared: Boolean,
    val isTransfer: Boolean,
)

// Mirrors GET /api/v1/accounts/:id/transactions?month=… — a month of ledger rows.
data class TransactionsPage(
    val month: String,
    val prevMonth: String,
    val nextMonth: String,
    val transactions: List<TransactionItem>,
)

// Mirrors GET /api/v1/categories — an option in the transaction category picker.
data class CategoryOption(val id: Long, val name: String, val group: String)

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

    suspend fun accounts(token: String, month: String?): AccountsPage = withContext(Dispatchers.IO) {
        val path = if (month != null) "/api/v1/accounts?month=$month" else "/api/v1/accounts"
        val json = request("GET", path, body = null, token = token)
        val arr = json.getJSONArray("accounts")
        AccountsPage(
            month = json.getString("month"),
            prevMonth = json.getString("prevMonth"),
            nextMonth = json.getString("nextMonth"),
            accounts = List(arr.length()) { arr.getJSONObject(it).toAccount() },
        )
    }

    suspend fun transactions(token: String, accountId: Long, month: String?): TransactionsPage =
        withContext(Dispatchers.IO) {
            val path = if (month != null) {
                "/api/v1/accounts/$accountId/transactions?month=$month"
            } else {
                "/api/v1/accounts/$accountId/transactions"
            }
            val json = request("GET", path, body = null, token = token)
            val arr = json.getJSONArray("transactions")
            TransactionsPage(
                month = json.getString("month"),
                prevMonth = json.getString("prevMonth"),
                nextMonth = json.getString("nextMonth"),
                transactions = List(arr.length()) { arr.getJSONObject(it).toTransaction() },
            )
        }

    suspend fun categories(token: String): List<CategoryOption> = withContext(Dispatchers.IO) {
        val arr = request("GET", "/api/v1/categories", body = null, token = token).getJSONArray("categories")
        List(arr.length()) {
            val o = arr.getJSONObject(it)
            CategoryOption(o.getLong("id"), o.getString("name"), o.getString("group"))
        }
    }

    suspend fun createTransaction(
        token: String,
        accountId: Long,
        amount: String,
        type: String,
        categoryId: Long?,
        payee: String,
        notes: String,
        date: String,
    ): Unit = withContext(Dispatchers.IO) {
        val body = JSONObject()
            .put("date", date)
            .put("amount", amount)
            .put("type", type)
            .put("payee", payee)
            .put("notes", notes)
        if (categoryId != null) body.put("categoryId", categoryId)
        request("POST", "/api/v1/accounts/$accountId/transactions", body = body.toString(), token = token)
        Unit
    }

    suspend fun updateTransaction(
        token: String,
        accountId: Long,
        transactionId: Long,
        amount: String,
        type: String,
        categoryId: Long?,
        payee: String,
        notes: String,
        date: String,
    ): Unit = withContext(Dispatchers.IO) {
        val body = JSONObject()
            .put("date", date)
            .put("amount", amount)
            .put("type", type)
            .put("payee", payee)
            .put("notes", notes)
        if (categoryId != null) body.put("categoryId", categoryId)
        request("PUT", "/api/v1/accounts/$accountId/transactions/$transactionId", body = body.toString(), token = token)
        Unit
    }

    private fun JSONObject.toUser() = User(getLong("id"), getString("email"), getString("name"))

    private fun JSONObject.toAccount() =
        Account(getLong("id"), getString("name"), getString("type"), getLong("balanceCents"))

    private fun JSONObject.toTransaction() = TransactionItem(
        id = getLong("id"),
        date = getString("date"),
        payee = getString("payee"),
        category = getString("category"),
        categoryId = if (isNull("categoryId")) null else getLong("categoryId"),
        notes = getString("notes"),
        amountCents = getLong("amountCents"),
        cleared = getBoolean("cleared"),
        isTransfer = optBoolean("isTransfer", false),
    )

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
