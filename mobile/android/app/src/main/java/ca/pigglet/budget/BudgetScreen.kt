package ca.pigglet.budget

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
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowLeft
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
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
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import java.time.YearMonth
import java.time.format.DateTimeFormatter
import java.util.Locale

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun BudgetScreen(
    month: String,
    onMonthChange: (String) -> Unit,
    loadBudget: suspend (String?) -> BudgetData,
    assign: suspend (Long, String, String) -> BudgetData,
) {
    var budget by remember { mutableStateOf<BudgetData?>(null) }
    var loading by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }

    var editing by remember { mutableStateOf<BudgetCategory?>(null) }
    var editText by remember { mutableStateOf("") }
    val scope = rememberCoroutineScope()

    var refreshing by remember { mutableStateOf(false) }

    suspend fun fetch() {
        error = null
        try {
            budget = loadBudget(month)
        } catch (e: ApiException) {
            error = e.message
        } catch (e: Exception) {
            error = "Couldn't load the budget."
        }
    }

    LaunchedEffect(month) {
        loading = true
        fetch()
        loading = false
    }

    val current = budget
    when {
        current != null -> PullToRefreshBox(
            isRefreshing = refreshing,
            onRefresh = { scope.launch { refreshing = true; fetch(); refreshing = false } },
        ) {
            BudgetContent(
                budget = current,
                onPrev = { onMonthChange(current.prevMonth) },
                onNext = { onMonthChange(current.nextMonth) },
                onCategoryClick = {
                    editing = it
                    editText = Money.plain(it.assignedCents)
                },
            )
        }

        loading -> Box(Modifier.fillMaxSize(), Alignment.Center) { CircularProgressIndicator() }

        error != null -> Box(Modifier.fillMaxSize(), Alignment.Center) {
            Column(horizontalAlignment = Alignment.CenterHorizontally) {
                Text(error ?: "", color = MaterialTheme.colorScheme.onSurfaceVariant)
                Button(onClick = { scope.launch { loading = true; fetch(); loading = false } }) { Text("Retry") }
            }
        }
    }

    val cat = editing
    if (cat != null) {
        AlertDialog(
            onDismissRequest = { editing = null },
            title = { Text("Assign to ${cat.name}") },
            text = {
                OutlinedTextField(
                    value = editText,
                    onValueChange = { editText = it },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
                )
            },
            confirmButton = {
                TextButton(onClick = {
                    val month = budget?.month ?: return@TextButton
                    scope.launch {
                        try {
                            budget = assign(cat.id, month, editText)
                        } catch (e: ApiException) {
                            error = e.message
                        } catch (e: Exception) {
                            error = "Couldn't save the assignment."
                        }
                        editing = null
                    }
                }) { Text("Save") }
            },
            dismissButton = { TextButton(onClick = { editing = null }) { Text("Cancel") } },
        )
    }

    // Surface errors once a budget is on screen (assign or month-nav failures).
    // The initial-load failure keeps its retry box above.
    val err = error
    if (err != null && budget != null) {
        AlertDialog(
            onDismissRequest = { error = null },
            confirmButton = { TextButton(onClick = { error = null }) { Text("OK") } },
            text = { Text(err) },
        )
    }
}

@Composable
private fun BudgetContent(
    budget: BudgetData,
    onPrev: () -> Unit,
    onNext: () -> Unit,
    onCategoryClick: (BudgetCategory) -> Unit,
) {
    Column(Modifier.fillMaxSize()) {
        MonthHeader(monthLabel(budget.month), onPrev, onNext)
        LazyColumn(Modifier.fillMaxSize()) {
            item { SummaryCard(budget.summary) }
            budget.groups.forEach { group ->
                item { GroupHeader(group.name) }
                if (group.categories.isEmpty()) {
                    item {
                        Text(
                            "No categories",
                            Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                } else {
                    items(group.categories, key = { it.id }) { cat ->
                        CategoryRow(cat, onClick = { onCategoryClick(cat) })
                    }
                }
            }
        }
    }
}

@Composable
private fun MonthHeader(label: String, onPrev: () -> Unit, onNext: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().padding(horizontal = 8.dp, vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        IconButton(onClick = onPrev) {
            Icon(Icons.AutoMirrored.Filled.KeyboardArrowLeft, contentDescription = "Previous month")
        }
        Text(
            label,
            Modifier.weight(1f),
            style = MaterialTheme.typography.titleMedium,
            fontWeight = FontWeight.SemiBold,
        )
        IconButton(onClick = onNext) {
            Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, contentDescription = "Next month")
        }
    }
}

@Composable
private fun SummaryCard(summary: BudgetSummary) {
    Column(Modifier.fillMaxWidth().padding(16.dp)) {
        SummaryRow("Income", summary.incomeCents, MaterialTheme.colorScheme.onSurface)
        SummaryRow("Assigned", summary.budgetedCents, MaterialTheme.colorScheme.onSurface)
        SummaryRow(
            "Left to assign",
            summary.remainingCents,
            if (summary.remainingCents < 0) Color(0xFFC62828) else Color(0xFF2E7D32),
        )
    }
    HorizontalDivider()
}

@Composable
private fun SummaryRow(label: String, cents: Long, tint: Color) {
    Row(Modifier.fillMaxWidth().padding(vertical = 2.dp)) {
        Text(label, Modifier.weight(1f))
        Text(Money.format(cents), color = tint)
    }
}

@Composable
private fun GroupHeader(name: String) {
    Text(
        name,
        Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
        style = MaterialTheme.typography.titleSmall,
        fontWeight = FontWeight.SemiBold,
        color = MaterialTheme.colorScheme.primary,
    )
}

@Composable
private fun CategoryRow(cat: BudgetCategory, onClick: () -> Unit) {
    Row(
        Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(horizontal = 16.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(cat.name)
            Text(
                "Assigned ${Money.format(cat.assignedCents)} · Spent ${Money.format(cat.spentCents)}",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Text(Money.format(cat.availableCents), color = availableColor(cat.availableCents))
    }
}

private fun availableColor(cents: Long): Color = when {
    cents > 0 -> Color(0xFF2E7D32)
    cents < 0 -> Color(0xFFC62828)
    else -> Color.Gray
}

// monthLabel turns "2026-07" into "July 2026".
private fun monthLabel(key: String): String = try {
    YearMonth.parse(key).format(DateTimeFormatter.ofPattern("MMMM yyyy", Locale.US))
} catch (e: Exception) {
    key
}
