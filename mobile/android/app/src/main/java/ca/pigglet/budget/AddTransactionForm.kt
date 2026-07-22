package ca.pigglet.budget

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExposedDropdownMenuAnchorType
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.rememberDatePickerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter
import java.util.Locale

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AddTransactionScreen(
    categories: List<CategoryOption>,
    saving: Boolean,
    error: String?,
    onCancel: () -> Unit,
    onSubmit: (amount: String, type: String, categoryId: Long?, payee: String, notes: String, date: String) -> Unit,
) {
    var amountCents by remember { mutableStateOf(0L) }
    var isExpense by remember { mutableStateOf(true) }
    var payee by remember { mutableStateOf("") }
    var notes by remember { mutableStateOf("") }
    var categoryId by remember { mutableStateOf<Long?>(null) }
    var expanded by remember { mutableStateOf(false) }

    var showDatePicker by remember { mutableStateOf(false) }
    val dateState = rememberDatePickerState(initialSelectedDateMillis = todayUtcMillis())
    val dateMillis = dateState.selectedDateMillis ?: todayUtcMillis()

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("Add Transaction") },
                navigationIcon = {
                    IconButton(onClick = onCancel) {
                        Icon(Icons.Filled.Close, contentDescription = "Cancel")
                    }
                },
                actions = {
                    TextButton(
                        onClick = {
                            onSubmit(
                                Money.plain(amountCents),
                                if (isExpense) "expense" else "income",
                                categoryId,
                                payee,
                                notes,
                                isoDate(dateMillis),
                            )
                        },
                        enabled = !saving && amountCents > 0,
                    ) { Text("Add") }
                },
            )
        },
    ) { padding ->
        Column(
            Modifier
                .padding(padding)
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                FilterChip(selected = isExpense, onClick = { isExpense = true }, label = { Text("Expense") })
                FilterChip(selected = !isExpense, onClick = { isExpense = false }, label = { Text("Income") })
            }

            Text(
                Money.format(amountCents),
                style = MaterialTheme.typography.displayMedium,
                color = if (amountCents == 0L) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.onSurface,
                textAlign = TextAlign.Center,
                modifier = Modifier.fillMaxWidth(),
            )

            // Numbers-only keypad (accumulator): each digit shifts in from the right.
            AmountKeypad(
                onDigit = { n -> if (amountCents < 100_000_000L) amountCents = amountCents * 10 + n },
                onBackspace = { amountCents /= 10 },
            )

            OutlinedButton(onClick = { showDatePicker = true }, modifier = Modifier.fillMaxWidth()) {
                Text("Date: ${displayDate(dateMillis)}")
            }

            ExposedDropdownMenuBox(expanded = expanded, onExpandedChange = { expanded = it }) {
                val selectedName = categories.firstOrNull { it.id == categoryId }?.name ?: "None"
                OutlinedTextField(
                    value = selectedName,
                    onValueChange = {},
                    readOnly = true,
                    label = { Text("Category") },
                    trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(expanded = expanded) },
                    modifier = Modifier.menuAnchor(ExposedDropdownMenuAnchorType.PrimaryNotEditable).fillMaxWidth(),
                )
                ExposedDropdownMenu(expanded = expanded, onDismissRequest = { expanded = false }) {
                    DropdownMenuItem(text = { Text("None") }, onClick = { categoryId = null; expanded = false })
                    categories.forEach { category ->
                        DropdownMenuItem(
                            text = { Text("${category.group} · ${category.name}") },
                            onClick = { categoryId = category.id; expanded = false },
                        )
                    }
                }
            }

            OutlinedTextField(
                value = payee,
                onValueChange = { payee = it },
                label = { Text("Payee") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            OutlinedTextField(
                value = notes,
                onValueChange = { notes = it },
                label = { Text("Notes") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )

            if (error != null) {
                Text(error, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
            }
        }
    }

    if (showDatePicker) {
        DatePickerDialog(
            onDismissRequest = { showDatePicker = false },
            confirmButton = { TextButton(onClick = { showDatePicker = false }) { Text("OK") } },
            dismissButton = { TextButton(onClick = { showDatePicker = false }) { Text("Cancel") } },
        ) {
            DatePicker(state = dateState)
        }
    }
}

@Composable
private fun AmountKeypad(onDigit: (Long) -> Unit, onBackspace: () -> Unit) {
    val rows = listOf(listOf(7, 8, 9), listOf(4, 5, 6), listOf(1, 2, 3))
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        rows.forEach { row ->
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
                row.forEach { n ->
                    OutlinedButton(onClick = { onDigit(n.toLong()) }, modifier = Modifier.weight(1f).height(56.dp)) {
                        Text("$n", style = MaterialTheme.typography.titleLarge)
                    }
                }
            }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
            Spacer(Modifier.weight(1f))
            OutlinedButton(onClick = { onDigit(0L) }, modifier = Modifier.weight(1f).height(56.dp)) {
                Text("0", style = MaterialTheme.typography.titleLarge)
            }
            OutlinedButton(onClick = onBackspace, modifier = Modifier.weight(1f).height(56.dp)) {
                Text("⌫", style = MaterialTheme.typography.titleLarge)
            }
        }
    }
}

private fun todayUtcMillis(): Long =
    LocalDate.now().atStartOfDay(ZoneOffset.UTC).toInstant().toEpochMilli()

private fun utcDate(millis: Long): LocalDate =
    Instant.ofEpochMilli(millis).atZone(ZoneOffset.UTC).toLocalDate()

private fun displayDate(millis: Long): String =
    utcDate(millis).format(DateTimeFormatter.ofPattern("MMM d, yyyy", Locale.US))

private fun isoDate(millis: Long): String = utcDate(millis).toString() // yyyy-MM-dd
