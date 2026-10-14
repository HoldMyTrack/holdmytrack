package dev.holdmytrack.android.net

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ApiExceptionTest {

    @Test
    fun `the API's error shape gives its message and keeps its code`() {
        val e = ApiException.from(422, "{\"error\":\"photo_needs_place\",\"message\":\"Where was it taken?\"}\n")
        assertEquals("Where was it taken?", e.message)
        assertEquals("photo_needs_place", e.errorCode)
        assertEquals(422, e.code)
    }

    @Test
    fun `a code without a message says the request failed, not the JSON`() {
        val e = ApiException.from(500, "{\"error\":\"internal\"}")
        assertEquals("request failed (500)", e.message)
        assertEquals("internal", e.errorCode)
    }

    @Test
    fun `a body that isn't the API's shape is shown as it is, with no code`() {
        val e = ApiException.from(502, "Bad Gateway")
        assertEquals("Bad Gateway", e.message)
        assertNull(e.errorCode)
        assertEquals("request failed (503)", ApiException.from(503, "").message)
    }
}
