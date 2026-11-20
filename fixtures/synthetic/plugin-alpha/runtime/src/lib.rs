pub fn available() -> bool { true }
#[cfg(test)]
mod tests {
    #[test]
    fn remains_available() { assert!(super::available()); }
}
