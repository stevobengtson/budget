package ca.pigglet.budget

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

// Dev server base URL. Reached via an adb reverse tunnel:
//   adb reverse tcp:8080 tcp:8080
// which forwards the device/emulator's localhost:8080 to the Mac's 127.0.0.1:8080
// over adb — firewall-proof, and works for physical USB devices too. Re-run the
// command after an emulator cold boot (it doesn't persist). Without the tunnel,
// the emulator-only alternative is http://10.0.2.2:8080 (needs the macOS firewall
// to allow the server's incoming connections).
const val BASE_URL = "http://localhost:8080"

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
