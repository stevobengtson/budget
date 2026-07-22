package com.plainlysoftware.pigglet

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.lifecycle.viewmodel.compose.viewModel

// API base URL per build type (see build.gradle.kts): debug → local dev server,
// release → production. In debug, the emulator/USB device reaches the Mac's
// localhost:8080 via `adb reverse tcp:8080 tcp:8080`.
val BASE_URL: String = BuildConfig.API_BASE_URL

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            MaterialTheme {
                Surface(modifier = Modifier.fillMaxSize()) {
                    val vm: AuthViewModel = viewModel()
                    when (val s = vm.state) {
                        is AuthState.Loading -> LoadingScreen()
                        is AuthState.SignedOut -> LoginScreen(
                            submitting = vm.submitting,
                            error = s.error,
                            onSignIn = vm::signIn,
                        )
                        is AuthState.SignedIn -> MainScreen(
                            user = s.user,
                            addOns = s.addOns,
                            onSignOut = vm::signOut,
                            loadBudget = vm::loadBudget,
                            assign = vm::assign,
                            loadAccounts = vm::loadAccounts,
                            loadTransactions = vm::loadTransactions,
                            loadCategories = vm::loadCategories,
                            createTransaction = vm::createTransaction,
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun LoadingScreen() {
    Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        CircularProgressIndicator()
    }
}
