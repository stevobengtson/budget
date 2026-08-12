import SwiftUI

struct LoginView: View {
    @EnvironmentObject private var session: Session
    @State private var email = ""
    @State private var password = ""
    @State private var submitting = false

    var body: some View {
        VStack(spacing: 20) {
            Image(systemName: "banknote")
                .font(.system(size: 48))
                .foregroundStyle(.tint)
            Text("Budget")
                .font(.largeTitle.bold())

            TextField("Email", text: $email)
                .textContentType(.username)
                .keyboardType(.emailAddress)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .textFieldStyle(.roundedBorder)

            SecureField("Password", text: $password)
                .textContentType(.password)
                .textFieldStyle(.roundedBorder)

            if let error = session.loginError {
                Text(error)
                    .foregroundStyle(Color.appDestructive)
                    .font(.footnote)
            }

            Button(action: submit) {
                if submitting {
                    ProgressView()
                } else {
                    Text("Sign in").frame(maxWidth: .infinity)
                }
            }
            .buttonStyle(.borderedProminent)
            .disabled(submitting || email.isEmpty || password.isEmpty)
        }
        .padding()
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color.appBackground.ignoresSafeArea())
    }

    private func submit() {
        submitting = true
        Task {
            await session.signIn(email: email, password: password)
            submitting = false
        }
    }
}
