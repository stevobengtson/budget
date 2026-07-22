package com.plainlysoftware.pigglet

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

// Stores the session token encrypted with an AES key held in the Android
// Keystore (hardware-backed where available), so the token isn't readable at
// rest. Ciphertext + IV live in a private SharedPreferences file.
//
// Replaces the deprecated EncryptedSharedPreferences with plain platform crypto,
// so there are no extra Gradle dependencies.
class TokenStore(context: Context) {

    private val prefs = context.getSharedPreferences("budget_secure", Context.MODE_PRIVATE)

    fun save(token: String) {
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, getOrCreateKey())
        val encrypted = cipher.doFinal(token.toByteArray(Charsets.UTF_8))
        prefs.edit()
            .putString(KEY_IV, encode(cipher.iv))
            .putString(KEY_DATA, encode(encrypted))
            .apply()
    }

    fun load(): String? {
        val iv = prefs.getString(KEY_IV, null) ?: return null
        val data = prefs.getString(KEY_DATA, null) ?: return null
        return try {
            val cipher = Cipher.getInstance(TRANSFORMATION)
            cipher.init(Cipher.DECRYPT_MODE, getOrCreateKey(), GCMParameterSpec(TAG_BITS, decode(iv)))
            String(cipher.doFinal(decode(data)), Charsets.UTF_8)
        } catch (e: Exception) {
            // Key rotated/lost or ciphertext corrupt — drop it and force re-login.
            clear()
            null
        }
    }

    fun clear() {
        prefs.edit().remove(KEY_IV).remove(KEY_DATA).apply()
    }

    private fun getOrCreateKey(): SecretKey {
        val keystore = KeyStore.getInstance(ANDROID_KEYSTORE).apply { load(null) }
        (keystore.getEntry(KEY_ALIAS, null) as? KeyStore.SecretKeyEntry)?.let { return it.secretKey }

        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, ANDROID_KEYSTORE)
        generator.init(
            KeyGenParameterSpec.Builder(
                KEY_ALIAS,
                KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT,
            )
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .build(),
        )
        return generator.generateKey()
    }

    private fun encode(bytes: ByteArray) = Base64.encodeToString(bytes, Base64.NO_WRAP)
    private fun decode(value: String): ByteArray = Base64.decode(value, Base64.NO_WRAP)

    private companion object {
        const val ANDROID_KEYSTORE = "AndroidKeyStore"
        const val KEY_ALIAS = "budget_token_key"
        const val TRANSFORMATION = "AES/GCM/NoPadding"
        const val TAG_BITS = 128
        const val KEY_IV = "iv"
        const val KEY_DATA = "data"
    }
}
