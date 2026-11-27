package com.rho.client.ui

import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.longClick
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.test.swipeUp
import androidx.compose.ui.test.hasScrollAction
import com.rho.client.net.PermissionRequest
import com.rho.client.ui.theme.RhoTheme
import com.rho.client.ui.theme.Scheme
import kotlinx.serialization.json.JsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner

/**
 * The gestures on the permission prompt, driven through Compose: holding
 * approves a dangerous call, a tap does not, and an answer that is waiting for
 * a connection shows that it is waiting rather than looking ignored.
 */
@RunWith(RobolectricTestRunner::class)
class PermissionSheetTest {

    @get:Rule
    val compose = createComposeRule()

    private fun request(risk: String, summary: String = "rm -rf build") = PermissionRequest(
        id = JsonPrimitive(1),
        requestId = "r1",
        tool = "bash",
        summary = summary,
        risk = risk,
    )

    private fun show(
        risk: String,
        summary: String = "rm -rf build",
        waiting: Boolean = false,
    ): MutableList<Boolean> {
        val answers = mutableListOf<Boolean>()
        compose.setContent {
            RhoTheme(scheme = Scheme.Verdigris, dark = false) {
                PermissionSheet(request(risk, summary), onAnswer = { answers += it }, waiting = waiting)
            }
        }
        compose.waitForIdle()
        return answers
    }

    @Test
    fun `holding approves a dangerous call`() {
        val answers = show("high")
        compose.onNodeWithText("Hold to approve").performTouchInput { longClick() }
        compose.waitForIdle()
        assertEquals(listOf(true), answers)
    }

    @Test
    fun `holding approves a long dangerous call once it is read to the end`() {
        val long = (1..80).joinToString("\n") { "rm -rf build/part-$it" }
        val answers = show("high", long)
        compose.onNodeWithText("Read it first").performTouchInput { longClick() }
        compose.waitForIdle()
        assertEquals("approving before reading must do nothing", emptyList<Boolean>(), answers)

        repeat(30) {
            compose.onNode(hasScrollAction()).performTouchInput { swipeUp(durationMillis = 300) }
            compose.waitForIdle()
        }
        compose.onNodeWithText("Hold to approve").performTouchInput { longClick() }
        compose.waitForIdle()
        assertEquals(listOf(true), answers)
    }

    @Test
    fun `tapping does not approve a dangerous call, and says so`() {
        val answers = show("high")
        compose.onNodeWithText("Hold to approve").performClick()
        compose.waitForIdle()
        assertEquals(emptyList<Boolean>(), answers)
        compose.onNodeWithText("Press and hold — a tap will not do it.").assertExists()
    }

    @Test
    fun `a tap approves a low risk call`() {
        val answers = show("low")
        compose.onNodeWithText("Approve").performClick()
        compose.waitForIdle()
        assertEquals(listOf(true), answers)
    }

    @Test
    fun `deny is one tap`() {
        val answers = show("high")
        compose.onNodeWithText("Deny").performClick()
        compose.waitForIdle()
        assertEquals(listOf(false), answers)
    }

    @Test
    fun `an answer waiting for a connection says so and takes no second answer`() {
        val answers = show("high", waiting = true)
        compose.onNodeWithText(WAITING_NOTE).assertExists()
        compose.onNodeWithText("Hold to approve").performTouchInput { longClick() }
        compose.onNodeWithText("Deny").performClick()
        compose.waitForIdle()
        assertEquals(emptyList<Boolean>(), answers)
    }
}
