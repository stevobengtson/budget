package com.plainlysoftware.pigglet

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.Person
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.Star
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.dp

// A bottom-nav destination. Transactions are reached by drilling into an account
// (Accounts tab), matching the web's account-scoped model.
// Icons are placeholders from the core set (no material-icons-extended dep);
// swap for budget/accounts/paydown-specific glyphs later.
private enum class Tab(val label: String, val icon: ImageVector) {
    Budget("Budget", Icons.Filled.Home),
    Accounts("Accounts", Icons.Filled.Person),
//    Paydown("Paydown", Icons.Filled.Star),
    Settings("Settings", Icons.Filled.Settings),
}

@Composable
fun MainScreen(
    user: User,
    addOns: List<String>,
    onSignOut: () -> Unit,
    loadBudget: suspend (String?) -> BudgetData,
    assign: suspend (Long, String, String) -> BudgetData,
    loadAccounts: suspend () -> List<Account>,
    loadTransactions: suspend (Long, String?) -> TransactionsPage,
    loadCategories: suspend () -> List<CategoryOption>,
    createTransaction: suspend (Long, String, String, Long?, String, String, String) -> Unit,
) {
    // Paydown appears only when its add-on is enabled.
//    val tabs = remember(addOns) {
//        Tab.entries.filter { it != Tab.Paydown || "paydown" in addOns }
//    }
    val tabs = remember() { Tab.entries }
    var selected by remember { mutableStateOf(Tab.Budget) }

    Scaffold(
        bottomBar = {
            NavigationBar {
                tabs.forEach { tab ->
                    NavigationBarItem(
                        selected = selected == tab,
                        onClick = { selected = tab },
                        icon = { Icon(tab.icon, contentDescription = tab.label) },
                        label = { Text(tab.label) },
                    )
                }
            }
        },
    ) { padding ->
        Box(modifier = Modifier.fillMaxSize().padding(padding)) {
            when (selected) {
                Tab.Budget -> BudgetScreen(loadBudget = loadBudget, assign = assign)
                Tab.Accounts -> AccountsScreen(
                    loadAccounts = loadAccounts,
                    loadTransactions = loadTransactions,
                    loadCategories = loadCategories,
                    createTransaction = createTransaction,
                )
//                Tab.Paydown -> ComingSoon("Paydown")
                Tab.Settings -> SettingsContent(user = user, onSignOut = onSignOut)
            }
        }
    }
}

// Placeholder for tabs not yet built (real budget page = Step 4).
@Composable
private fun ComingSoon(title: String) {
    Column(
        modifier = Modifier.fillMaxSize().padding(24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Text(title, style = MaterialTheme.typography.headlineMedium)
        Spacer(Modifier.padding(4.dp))
        Text("Coming soon", color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

@Composable
private fun SettingsContent(user: User, onSignOut: () -> Unit) {
    Column(
        modifier = Modifier.fillMaxSize().padding(24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Text(user.name, style = MaterialTheme.typography.titleLarge)
        Spacer(Modifier.padding(4.dp))
        Text(user.email, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Spacer(Modifier.padding(12.dp))
        OutlinedButton(onClick = onSignOut) { Text("Sign out") }
    }
}
