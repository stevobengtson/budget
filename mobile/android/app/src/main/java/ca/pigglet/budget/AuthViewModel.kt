package ca.pigglet.budget

import android.app.Application
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.launch

// AuthState drives which screen shows.
sealed interface AuthState {
    data object Loading : AuthState
    data class SignedOut(val error: String? = null) : AuthState
    data class SignedIn(val user: User, val addOns: List<String>) : AuthState
}

// AuthViewModel owns the token + auth state and survives configuration changes.
// AndroidViewModel gives it the application context the TokenStore needs.
class AuthViewModel(app: Application) : AndroidViewModel(app) {

    private val api = ApiClient(BASE_URL)
    private val tokens = TokenStore(app)

    var state by mutableStateOf<AuthState>(AuthState.Loading)
        private set
    var submitting by mutableStateOf(false)
        private set

    init { bootstrap() }

    // bootstrap restores a saved session on launch: if a stored token still
    // authenticates, go straight to signed-in; otherwise sign out.
    private fun bootstrap() {
        val token = tokens.load()
        if (token == null) {
            state = AuthState.SignedOut()
            return
        }
        viewModelScope.launch {
            state = try {
                val me = api.me(token)
                AuthState.SignedIn(me.user, me.addOns)
            } catch (e: Exception) {
                tokens.clear()
                AuthState.SignedOut()
            }
        }
    }

    fun signIn(email: String, password: String) {
        submitting = true
        viewModelScope.launch {
            try {
                val result = api.login(email, password)
                tokens.save(result.token)
                state = AuthState.SignedIn(result.user, result.addOns)
            } catch (e: ApiException) {
                state = AuthState.SignedOut(e.message)
            } catch (e: Exception) {
                state = AuthState.SignedOut("Something went wrong.")
            } finally {
                submitting = false
            }
        }
    }

    // loadBudget fetches a month's budget (null = current) with the stored token,
    // keeping the token encapsulated here.
    suspend fun loadBudget(month: String?): BudgetData {
        val token = tokens.load() ?: throw ApiException("unauthorized", "Not signed in.")
        return api.budget(token, month)
    }

    fun signOut() {
        viewModelScope.launch {
            tokens.load()?.let { api.logout(it) }
            tokens.clear()
            state = AuthState.SignedOut()
        }
    }
}
