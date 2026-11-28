package com.rho.client.ui

import kotlinx.serialization.json.JsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * An answer given while the phone has no live connection must still reach the
 * daemon, and a prompt asked again after a lost answer must not ask twice.
 */
class PermissionAnswersTest {

    @Test
    fun `an answer stays unsent until a connection takes it`() {
        val a = PermissionAnswers()
        a.record("r1", JsonPrimitive("r1"), approve = true)
        assertEquals(setOf("r1"), a.unsent.value)
        assertEquals(listOf(true), a.pending().map { it.approve })

        a.sent("r1")
        assertTrue(a.unsent.value.isEmpty())
        assertTrue(a.pending().isEmpty())
    }

    @Test
    fun `a prompt asked again gets the answer already given, under its new id`() {
        val a = PermissionAnswers()
        a.record("r1", JsonPrimitive("old"), approve = true)
        a.sent("r1") // went out on a socket that was already dead

        val again = a.answerFor("r1", JsonPrimitive("new"))!!
        assertEquals(true, again.approve)
        assertEquals(JsonPrimitive("new"), again.rpcId)
    }

    @Test
    fun `a prompt nobody answered here is shown, not answered`() {
        assertNull(PermissionAnswers().answerFor("r9", JsonPrimitive("r9")))
    }

    @Test
    fun `a deny is remembered as a deny`() {
        val a = PermissionAnswers()
        a.record("r1", JsonPrimitive("r1"), approve = false)
        assertEquals(false, a.answerFor("r1", JsonPrimitive("r1"))!!.approve)
    }

    @Test
    fun `only the most recent answers are kept`() {
        val a = PermissionAnswers(remember = 2)
        a.record("r1", JsonPrimitive("r1"), approve = true)
        a.record("r2", JsonPrimitive("r2"), approve = true)
        a.record("r3", JsonPrimitive("r3"), approve = true)
        assertNull(a.answerFor("r1", JsonPrimitive("r1")))
        assertEquals(setOf("r2", "r3"), a.unsent.value)
    }
}
