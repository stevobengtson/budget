package ca.pigglet.budget

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Add
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import kotlinx.coroutines.launch
import java.time.LocalDate
import java.time.format.DateTimeFormatter
import java.util.Locale

@Composable
fun AccountsScreen(
    loadAccounts: suspend () -> List<Account>,
    loadTransactions: suspend (Long) -> List<TransactionItem>,
    loadCategories: suspend () -> List<CategoryOption>,
    createTransaction: suspend (Long, String, String, Long?, String, String, String) -> Unit,
) {
    // In-tab drill-in: null = account list, non-null = that account's ledger.
    var selected by remember { mutableStateOf<Account?>(null) }
    val account = selected

    if (account == null) {
        AccountList(loadAccounts, onOpen = { selected = it })
    } else {
        AccountTransactions(account, loadTransactions, loadCategories, createTransaction, onBack = { selected = null })
        BackHandler { selected = null }
    }
}

@Composable
private fun AccountList(loadAccounts: suspend () -> List<Account>, onOpen: (Account) -> Unit) {
    var accounts by remember { mutableStateOf<List<Account>?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var attempt by remember { mutableStateOf(0) }

    LaunchedEffect(attempt) {
        error = null
        accounts = null
        try {
            accounts = loadAccounts()
        } catch (e: ApiException) {
            error = e.message
        } catch (e: Exception) {
            error = "Couldn't load accounts."
        }
    }

    val current = accounts
    when {
        current != null -> LazyColumn(Modifier.fillMaxSize()) {
            items(current, key = { it.id }) { account ->
                AccountRow(account, onClick = { onOpen(account) })
                HorizontalDivider()
            }
        }

        error != null -> ErrorBox(error) { attempt++ }
        else -> Loading()
    }
}

@Composable
private fun AccountRow(account: Account, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(16.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(account.name)
            Text(
                account.type.replaceFirstChar { it.uppercase() },
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Text(
            Money.format(account.balanceCents),
            color = if (account.balanceCents < 0) Color(0xFFC62828) else MaterialTheme.colorScheme.onSurface,
        )
    }
}

@Composable
private fun AccountTransactions(
    account: Account,
    loadTransactions: suspend (Long) -> List<TransactionItem>,
    loadCategories: suspend () -> List<CategoryOption>,
    createTransaction: suspend (Long, String, String, Long?, String, String, String) -> Unit,
    onBack: () -> Unit,
) {
    var txs by remember(account.id) { mutableStateOf<List<TransactionItem>?>(null) }
    var error by remember(account.id) { mutableStateOf<String?>(null) }
    var attempt by remember(account.id) { mutableStateOf(0) }

    var showAdd by remember { mutableStateOf(false) }
    var categories by remember { mutableStateOf<List<CategoryOption>>(emptyList()) }
    var saving by remember { mutableStateOf(false) }
    var addError by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()

    LaunchedEffect(account.id, attempt) {
        error = null
        txs = null
        try {
            txs = loadTransactions(account.id)
        } catch (e: ApiException) {
            error = e.message
        } catch (e: Exception) {
            error = "Couldn't load transactions."
        }
    }

    // Load the category picker the first time the form opens.
    LaunchedEffect(showAdd) {
        if (showAdd && categories.isEmpty()) {
            categories = runCatching { loadCategories() }.getOrDefault(emptyList())
        }
    }

    Column(Modifier.fillMaxSize()) {
        Row(
            Modifier.fillMaxWidth().padding(horizontal = 4.dp, vertical = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconButton(onClick = onBack) {
                Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Back")
            }
            Text(
                account.name,
                Modifier.weight(1f),
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.SemiBold,
            )
            IconButton(onClick = { addError = null; showAdd = true }) {
                Icon(Icons.Filled.Add, contentDescription = "Add transaction")
            }
        }
        HorizontalDivider()

        val current = txs
        when {
            current != null && current.isEmpty() -> Box(Modifier.fillMaxSize(), Alignment.Center) {
                Text("No transactions", color = MaterialTheme.colorScheme.onSurfaceVariant)
            }

            current != null -> LazyColumn(Modifier.fillMaxSize()) {
                items(current, key = { it.id }) { tx ->
                    TransactionRow(tx)
                    HorizontalDivider()
                }
            }

            error != null -> ErrorBox(error) { attempt++ }
            else -> Loading()
        }
    }

    if (showAdd) {
        // Full-screen route (overlay), so the ledger's state is preserved
        // underneath and we land back on it after adding.
        Dialog(
            onDismissRequest = { showAdd = false },
            properties = DialogProperties(usePlatformDefaultWidth = false),
        ) {
            Surface(Modifier.fillMaxSize()) {
                AddTransactionScreen(
                    categories = categories,
                    saving = saving,
                    error = addError,
                    onCancel = { showAdd = false },
                    onSubmit = { amount, type, categoryId, payee, notes, date ->
                        saving = true
                        addError = null
                        scope.launch {
                            try {
                                createTransaction(account.id, amount, type, categoryId, payee, notes, date)
                                showAdd = false
                                attempt++ // reload the ledger with the new transaction
                            } catch (e: ApiException) {
                                addError = e.message
                            } catch (e: Exception) {
                                addError = "Couldn't save the transaction."
                            }
                            saving = false
                        }
                    },
                )
            }
        }
    }
}

@Composable
private fun TransactionRow(tx: TransactionItem) {
    Row(
        Modifier.fillMaxWidth().padding(16.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(tx.payee.ifEmpty { tx.category.ifEmpty { "—" } })
            Text(
                subtitle(tx),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Text(
            Money.format(tx.amountCents),
            color = if (tx.amountCents > 0) Color(0xFF2E7D32) else MaterialTheme.colorScheme.onSurface,
        )
    }
}

@Composable
private fun Loading() {
    Box(Modifier.fillMaxSize(), Alignment.Center) { CircularProgressIndicator() }
}

@Composable
private fun ErrorBox(message: String?, onRetry: () -> Unit) {
    Box(Modifier.fillMaxSize(), Alignment.Center) {
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Text(message ?: "", color = MaterialTheme.colorScheme.onSurfaceVariant)
            Button(onClick = onRetry) { Text("Retry") }
        }
    }
}

// subtitle shows "Category · Jul 15", dropping an empty category.
private fun subtitle(tx: TransactionItem): String {
    val date = shortDate(tx.date)
    return if (tx.category.isEmpty()) date else "${tx.category} · $date"
}

private fun shortDate(iso: String): String = try {
    LocalDate.parse(iso).format(DateTimeFormatter.ofPattern("MMM d", Locale.US))
} catch (e: Exception) {
    iso
}
